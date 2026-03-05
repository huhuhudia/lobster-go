package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/memory"
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

// ToolResultMaxChars caps tool output stored into history.
const ToolResultMaxChars = 500

// LoopConfig contains tunables for the agent loop.
type LoopConfig struct {
	MaxIterations          int
	Model                  string
	Temperature            float64
	MaxTokens              int
	ReasoningEffort        string
	Workspace              string
	RestrictToWorkspace    bool
	BraveAPIKey            string
	WebProxy               string
	ExecTimeoutSec         int
	MemoryConsolidateEvery int
	MemoryWindowSize       int
	MemoryMode             string
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
	bus         *bus.MessageBus
	provider    providers.Provider
	sessions    *session.Manager
	tools       *ToolRegistry
	ctxBuilder  ContextBuilder
	cfg         LoopConfig
	memoryStore *memory.Store

	active bool
	mu     sync.Mutex
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
	if cfg.MemoryWindowSize == 0 {
		cfg.MemoryWindowSize = 50
	}
	return &AgentLoop{
		bus:         bus,
		provider:    provider,
		sessions:    sessions,
		tools:       NewToolRegistry(),
		ctxBuilder:  builder,
		cfg:         cfg,
		memoryStore: buildMemoryStore(cfg),
	}
}

// RegisterTool adds a tool to the registry.
func (l *AgentLoop) RegisterTool(t Tool) { l.tools.Register(t) }

// Run starts a loop that consumes inbound messages until ctx canceled.
func (l *AgentLoop) Run(ctx context.Context) error {
	l.mu.Lock()
	if l.active {
		l.mu.Unlock()
		return nil
	}
	l.active = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.active = false
		l.mu.Unlock()
	}()

	for {
		msg, err := l.bus.ConsumeInbound(ctx)
		if err != nil {
			return err
		}
		// process each message in its own goroutine
		go l.handleMessage(ctx, msg)
	}
}

func (l *AgentLoop) handleMessage(ctx context.Context, in bus.InboundMessage) {
	sess, err := l.sessions.GetOrCreate(in.SessionKey())
	if err != nil {
		return
	}
	defer func() {
		_ = l.sessions.Save(sess)
	}()
	sess.AddMessage("user", in.Content)

	messages := l.ctxBuilder.Build(sess, l.tools.Definitions())
	iter := 0
	replied := false
	for {
		iter++
		if iter > l.cfg.MaxIterations {
			break
		}
		if ctx.Err() != nil {
			return
		}
		reqStart := time.Now()
		req := providers.ChatRequest{
			Messages:        messages,
			Tools:           l.tools.Definitions(),
			Model:           l.cfg.Model,
			Temperature:     l.cfg.Temperature,
			MaxTokens:       l.cfg.MaxTokens,
			ReasoningEffort: l.cfg.ReasoningEffort,
			Timeout:         time.Duration(l.cfg.ExecTimeoutSec) * time.Second,
		}
		if strings.TrimSpace(req.Model) == "" {
			req.Model = l.provider.DefaultModel()
		}
		resp, err := l.provider.Chat(ctx, req)
		if err != nil {
			meta := map[string]string{
				"latency_ms": fmt.Sprintf("%d", time.Since(reqStart).Milliseconds()),
			}
			if code := extractErrorCode(err); code != "" {
				meta["error_code"] = code
			}
			content := "error: provider chat failed: " + err.Error()
			sess.AddMessage("assistant", content)
			_ = l.bus.PublishOutbound(bus.OutboundMessage{
				Channel:  in.Channel,
				ChatID:   in.ChatID,
				Content:  content,
				Metadata: meta,
			})
			replied = true
			break
		}
		// If provider returns tool calls, execute all of them and continue the loop.
		if len(resp.Message.ToolCalls) > 0 {
			assistantToolMsg := providers.ChatMessage{
				Role:      "assistant",
				Content:   stringifyContent(resp.Message.Content),
				ToolCalls: resp.Message.ToolCalls,
			}
			messages = append(messages, assistantToolMsg)
			sess.AddMessage("assistant", assistantToolMsg.Content, session.WithToolCalls(resp.Message.ToolCalls))

			for _, tc := range resp.Message.ToolCalls {
				tool, ok := l.tools.Get(tc.Name)
				result := ""
				if !ok {
					result = "error: tool not found: " + tc.Name
				} else {
					toolOut, toolErr := tool.Execute(ctx, tc.Arguments)
					if toolErr != nil {
						result = "error: " + toolErr.Error()
					} else {
						result = toolOut
					}
				}
				if len(result) > ToolResultMaxChars {
					result = result[:ToolResultMaxChars]
				}
				toolMsg := providers.ChatMessage{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				}
				messages = append(messages, toolMsg)
				sess.AddMessage("tool", result, session.WithToolCallID(tc.ID), session.WithName(tc.Name))
			}
			continue
		}
		// final answer
		content := stringifyContent(resp.Message.Content)
		if strings.TrimSpace(content) == "" {
			content = "(empty response)"
		}
		sess.AddMessage("assistant", content)
		l.maybeConsolidate(ctx, &sess)
		meta := map[string]string{
			"latency_ms": fmt.Sprintf("%d", time.Since(reqStart).Milliseconds()),
		}
		if resp.Usage != nil {
			meta["prompt_tokens"] = fmt.Sprintf("%d", resp.Usage.PromptTokens)
			meta["completion_tokens"] = fmt.Sprintf("%d", resp.Usage.CompletionTokens)
			meta["total_tokens"] = fmt.Sprintf("%d", resp.Usage.TotalTokens)
		}
		_ = l.bus.PublishOutbound(bus.OutboundMessage{
			Channel:  in.Channel,
			ChatID:   in.ChatID,
			Content:  content,
			Metadata: meta,
		})
		replied = true
		break
	}
	if !replied && ctx.Err() == nil {
		content := "error: max iterations reached"
		sess.AddMessage("assistant", content)
		l.maybeConsolidate(ctx, &sess)
		_ = l.bus.PublishOutbound(bus.OutboundMessage{
			Channel: in.Channel,
			ChatID:  in.ChatID,
			Content: content,
		})
	}
}

func buildMemoryStore(cfg LoopConfig) *memory.Store {
	if cfg.MemoryConsolidateEvery <= 0 {
		return nil
	}
	if strings.TrimSpace(cfg.Workspace) == "" {
		return nil
	}
	return memory.NewStore(cfg.Workspace)
}

func (l *AgentLoop) maybeConsolidate(parent context.Context, sess *session.Session) {
	if l.memoryStore == nil || sess == nil {
		return
	}
	start := sess.LastConsolidated
	if start < 0 {
		start = 0
	}
	pending := len(sess.Messages) - start
	if pending < l.cfg.MemoryConsolidateEvery {
		return
	}
	mode := memory.ConsolidateMode(l.cfg.MemoryMode)
	if mode == "" {
		mode = memory.ConsolidateModeWindow
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	_, _ = l.memoryStore.ConsolidateSession(ctx, l.provider, sess, memory.ConsolidateOptions{
		Mode:        mode,
		WindowSize:  l.cfg.MemoryWindowSize,
		Model:       l.cfg.Model,
		MaxTokens:   l.cfg.MaxTokens,
		Temperature: l.cfg.Temperature,
	})
}

func stringifyContent(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

func extractErrorCode(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	idx := strings.Index(msg, "openai status ")
	if idx == -1 {
		return ""
	}
	part := msg[idx+len("openai status "):]
	fields := strings.Fields(part)
	if len(fields) == 0 {
		return ""
	}
	code := fields[0]
	for _, r := range code {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return code
}
