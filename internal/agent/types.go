package agent

import (
	"context"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

// ToolResultMaxChars caps tool output stored into history.
const ToolResultMaxChars = 500

// LoopConfig contains tunables for the agent loop.
type LoopConfig struct {
	MaxIterations        int
	Temperature          float64
	MaxTokens            int
	ReasoningEffort      string
	Workspace            string
	RestrictToWorkspace  bool
	BraveAPIKey          string
	WebProxy             string
	ExecTimeoutSec       int
}

// ContextBuilder builds prompts (placeholder; will be expanded).
type ContextBuilder interface {
	Build(sess session.Session, tools []ToolDefinition) []providers.ChatMessage
	BuildToolResult(content string) providers.ChatMessage
}

// ToolDefinition mirrors providers.ToolDefinition for registration convenience.
type ToolDefinition = providers.ToolDefinition

// Tool is the runtime executable tool.
type Tool interface {
	Name() string
	Definition() ToolDefinition
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolRegistry holds tools by name.
type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[string]Tool{}}
}

func (r *ToolRegistry) Register(t Tool) { r.tools[t.Name()] = t }

func (r *ToolRegistry) Definitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

func (r *ToolRegistry) Get(name string) (Tool, bool) { t, ok := r.tools[name]; return t, ok }

// AgentLoop ties together bus, provider, sessions and tools.
type AgentLoop struct {
	bus        *bus.MessageBus
	provider   providers.Provider
	sessions   *session.Manager
	tools      *ToolRegistry
	ctxBuilder ContextBuilder
	cfg        LoopConfig

	active   bool
	cancelFn context.CancelFunc
}

// NewLoop constructs an AgentLoop with dependencies.
func NewLoop(bus *bus.MessageBus, provider providers.Provider, sessions *session.Manager, builder ContextBuilder, cfg LoopConfig) *AgentLoop {
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 40
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.1
	}
	return &AgentLoop{
		bus:        bus,
		provider:   provider,
		sessions:   sessions,
		tools:      NewToolRegistry(),
		ctxBuilder: builder,
		cfg:        cfg,
	}
}

// RegisterTool adds a tool to the registry.
func (l *AgentLoop) RegisterTool(t Tool) { l.tools.Register(t) }

// Run starts a loop that consumes inbound messages until ctx canceled.
func (l *AgentLoop) Run(ctx context.Context) error {
	if l.active {
		return nil
	}
	l.active = true
	defer func() { l.active = false }()

	for {
		msg, err := l.bus.ConsumeInbound(ctx)
		if err != nil {
			return err
		}
		// process each message in its own goroutine
		go l.handleMessage(context.Background(), msg)
	}
}

func (l *AgentLoop) handleMessage(ctx context.Context, in bus.InboundMessage) {
	sess, err := l.sessions.GetOrCreate(in.SessionKey())
	if err != nil {
		return
	}
	sess.AddMessage("user", in.Content)

	messages := l.ctxBuilder.Build(sess, l.tools.Definitions())
	iter := 0
	for {
		iter++
		if iter > l.cfg.MaxIterations {
			break
		}
		req := providers.ChatRequest{
			Messages:        messages,
			Tools:           l.tools.Definitions(),
			Model:           l.provider.DefaultModel(),
			Temperature:     l.cfg.Temperature,
			MaxTokens:       l.cfg.MaxTokens,
			ReasoningEffort: l.cfg.ReasoningEffort,
			Timeout:         time.Duration(l.cfg.ExecTimeoutSec) * time.Second,
		}
		resp, err := l.provider.Chat(ctx, req)
		if err != nil {
			break
		}
		// If provider returns tool call, execute once.
		if len(resp.Message.ToolCalls) > 0 {
			tc := resp.Message.ToolCalls[0]
			tool, ok := l.tools.Get(tc.Name)
			if !ok {
				break
			}
			result, err := tool.Execute(ctx, tc.Arguments)
			if err != nil {
				result = "error: " + err.Error()
			}
			if len(result) > ToolResultMaxChars {
				result = result[:ToolResultMaxChars]
			}
			toolMsg := l.ctxBuilder.BuildToolResult(result)
			messages = append(messages, toolMsg)
			sess.AddMessage("tool", result)
			continue
		}
		// final answer
		content, _ := resp.Message.Content.(string)
		sess.AddMessage("assistant", content)
		_ = l.sessions.Save(sess)
		_ = l.bus.PublishOutbound(bus.OutboundMessage{
			Channel: in.Channel,
			ChatID:  in.ChatID,
			Content: content,
		})
		break
	}
}
