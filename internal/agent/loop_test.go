package agent

import (
	stdctx "context"
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
