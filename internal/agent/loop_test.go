package agent

import (
	stdctx "context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	builder "github.com/huhuhudia/lobster-go/internal/agent/context"
	"github.com/huhuhudia/lobster-go/internal/agent/tools"
	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

type twoStepProvider struct {
	toolName string
}

func (p *twoStepProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	// If the last message is a tool result, return final answer.
	if len(req.Messages) > 0 {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" {
			return providers.ChatResponse{
				Message: providers.ChatMessage{Role: "assistant", Content: "done"},
			}, nil
		}
	}
	// Otherwise request tool execution.
	return providers.ChatResponse{
		Message: providers.ChatMessage{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "1", Name: p.toolName, Arguments: map[string]interface{}{}},
			},
		},
		HasToolCall: true,
	}, nil
}

func (p *twoStepProvider) DefaultModel() string { return "mock" }

func TestLoopRunsToolThenAnswer(t *testing.T) {
	b := bus.New(10)
	sessDir := t.TempDir()
	smgr := session.NewManager(sessDir)

	prov := &twoStepProvider{toolName: "fake"}
	lp := NewLoop(b, prov, smgr, builder.DummyBuilder{}, LoopConfig{ExecTimeoutSec: 1})
	lp.RegisterTool(tools.FakeTool{Result: "tool-result"})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()

	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "done" {
		t.Fatalf("expected final content 'done', got %s", resp.Content)
	}
}

func TestLoopUnknownToolStillResponds(t *testing.T) {
	b := bus.New(10)
	smgr := session.NewManager(t.TempDir())
	prov := &twoStepProvider{toolName: "missing_tool"}
	lp := NewLoop(b, prov, smgr, builder.DummyBuilder{}, LoopConfig{ExecTimeoutSec: 1})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()

	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "done" {
		t.Fatalf("expected final content 'done', got %s", resp.Content)
	}
}

type multiToolProvider struct{}

func (p *multiToolProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	toolCount := 0
	for _, m := range req.Messages {
		if m.Role == "tool" {
			toolCount++
		}
	}
	if toolCount >= 2 {
		return providers.ChatResponse{Message: providers.ChatMessage{Role: "assistant", Content: "done-multi"}}, nil
	}
	return providers.ChatResponse{
		Message: providers.ChatMessage{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "1", Name: "tool_a", Arguments: map[string]interface{}{}},
				{ID: "2", Name: "tool_b", Arguments: map[string]interface{}{}},
			},
		},
		HasToolCall: true,
	}, nil
}

func (p *multiToolProvider) DefaultModel() string { return "mock" }

type namedTool struct {
	name   string
	result string
}

func (t namedTool) Name() string { return t.name }
func (t namedTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name": t.name,
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}
func (t namedTool) Execute(ctx stdctx.Context, args map[string]interface{}) (string, error) {
	return t.result, nil
}

func TestLoopExecutesAllToolCallsInOneTurn(t *testing.T) {
	b := bus.New(10)
	smgr := session.NewManager(t.TempDir())
	lp := NewLoop(b, &multiToolProvider{}, smgr, builder.DummyBuilder{}, LoopConfig{ExecTimeoutSec: 1})
	lp.RegisterTool(namedTool{name: "tool_a", result: "A"})
	lp.RegisterTool(namedTool{name: "tool_b", result: "B"})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "done-multi" {
		t.Fatalf("expected done-multi, got %s", resp.Content)
	}
}

type errProvider struct{}

func (p *errProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, errors.New("boom")
}
func (p *errProvider) DefaultModel() string { return "mock" }

func TestLoopProviderErrorPublishesError(t *testing.T) {
	b := bus.New(10)
	smgr := session.NewManager(t.TempDir())
	lp := NewLoop(b, &errProvider{}, smgr, builder.DummyBuilder{}, LoopConfig{ExecTimeoutSec: 1})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if !strings.Contains(resp.Content, "provider chat failed: boom") {
		t.Fatalf("expected provider error in outbound, got %s", resp.Content)
	}
}

type emptyProvider struct{}

func (p *emptyProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{
		Message: providers.ChatMessage{
			Role:    "assistant",
			Content: "   ",
		},
	}, nil
}
func (p *emptyProvider) DefaultModel() string { return "mock" }

func TestLoopEmptyAssistantResponseFallback(t *testing.T) {
	b := bus.New(10)
	smgr := session.NewManager(t.TempDir())
	lp := NewLoop(b, &emptyProvider{}, smgr, builder.DummyBuilder{}, LoopConfig{ExecTimeoutSec: 1})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "(empty response)" {
		t.Fatalf("expected empty fallback, got %s", resp.Content)
	}
}

type neverEndingToolProvider struct{}

func (p *neverEndingToolProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{
		Message: providers.ChatMessage{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "1", Name: "fake", Arguments: map[string]interface{}{}},
			},
		},
		HasToolCall: true,
	}, nil
}
func (p *neverEndingToolProvider) DefaultModel() string { return "mock" }

func TestLoopMaxIterationsFallback(t *testing.T) {
	b := bus.New(10)
	smgr := session.NewManager(t.TempDir())
	lp := NewLoop(b, &neverEndingToolProvider{}, smgr, builder.DummyBuilder{}, LoopConfig{MaxIterations: 1, ExecTimeoutSec: 1})
	lp.RegisterTool(tools.FakeTool{Result: "tool-result"})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "error: max iterations reached" {
		t.Fatalf("expected max-iterations fallback, got %s", resp.Content)
	}
}

type memoryAwareProvider struct{}

func (p *memoryAwareProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	for _, tool := range req.Tools {
		fn, ok := tool.Function["name"].(string)
		if ok && fn == "save_memory" {
			return providers.ChatResponse{
				Message: providers.ChatMessage{
					Role: "assistant",
					ToolCalls: []providers.ToolCall{
						{
							ID:   "m1",
							Name: "save_memory",
							Arguments: map[string]interface{}{
								"summary":       "auto-summary",
								"memory_update": "auto-memory",
							},
						},
					},
				},
				HasToolCall: true,
			}, nil
		}
	}
	return providers.ChatResponse{
		Message: providers.ChatMessage{Role: "assistant", Content: "assistant-reply"},
	}, nil
}

func (p *memoryAwareProvider) DefaultModel() string { return "mock" }

type strictToolSequenceProvider struct {
	step    int
	lastReq providers.ChatRequest
}

func (p *strictToolSequenceProvider) Chat(ctx stdctx.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	p.lastReq = req
	if p.step == 0 {
		p.step++
		return providers.ChatResponse{
			Message: providers.ChatMessage{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{ID: "tool-1", Name: "fake", Arguments: map[string]interface{}{}},
				},
			},
			HasToolCall: true,
		}, nil
	}

	foundAssistantWithCalls := false
	foundToolWithID := false
	for _, m := range req.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "tool-1" {
			foundAssistantWithCalls = true
		}
		if m.Role == "tool" && m.ToolCallID == "tool-1" {
			foundToolWithID = true
		}
	}
	if !foundAssistantWithCalls || !foundToolWithID {
		return providers.ChatResponse{}, errors.New("invalid tool message sequence")
	}

	return providers.ChatResponse{
		Message: providers.ChatMessage{Role: "assistant", Content: "strict-ok"},
	}, nil
}

func (p *strictToolSequenceProvider) DefaultModel() string { return "mock" }

func TestLoopAutoConsolidatesMemory(t *testing.T) {
	b := bus.New(10)
	workspace := t.TempDir()
	smgr := session.NewManager(filepath.Join(workspace, "sessions"))

	lp := NewLoop(b, &memoryAwareProvider{}, smgr, builder.DummyBuilder{}, LoopConfig{
		Workspace:              workspace,
		ExecTimeoutSec:         1,
		MemoryConsolidateEvery: 2,
		MemoryWindowSize:       10,
		MemoryMode:             "window",
	})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "assistant-reply" {
		t.Fatalf("unexpected assistant response: %s", resp.Content)
	}

	var sess session.Session
	found := false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		smgr.Invalidate("tg:1")
		sess, err = smgr.GetOrCreate("tg:1")
		if err == nil && sess.LastConsolidated >= 2 {
			found = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected LastConsolidated>=2, got %d", sess.LastConsolidated)
	}

	memBytes, err := os.ReadFile(filepath.Join(workspace, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if strings.TrimSpace(string(memBytes)) != "auto-memory" {
		t.Fatalf("unexpected memory content: %q", string(memBytes))
	}

	historyBytes, err := os.ReadFile(filepath.Join(workspace, "memory", "HISTORY.md"))
	if err != nil {
		t.Fatalf("read HISTORY.md: %v", err)
	}
	if !strings.Contains(string(historyBytes), "auto-summary") {
		t.Fatalf("history missing summary")
	}
}

func TestLoopBuildsValidToolSequenceForStrictProvider(t *testing.T) {
	b := bus.New(10)
	smgr := session.NewManager(t.TempDir())
	prov := &strictToolSequenceProvider{}
	lp := NewLoop(b, prov, smgr, builder.DummyBuilder{}, LoopConfig{ExecTimeoutSec: 1})
	lp.RegisterTool(tools.FakeTool{Result: "ok"})

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	defer cancel()
	if err := b.PublishInbound(bus.InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	go lp.Run(ctx)

	outCtx, outCancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer outCancel()
	resp, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if resp.Content != "strict-ok" {
		t.Fatalf("unexpected response: %s", resp.Content)
	}

	// validate assistant tool_calls schema
	found := false
	for _, msg := range prov.lastReq.Messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected assistant tool_calls message in request")
	}
}
