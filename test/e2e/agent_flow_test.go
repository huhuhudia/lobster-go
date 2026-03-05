//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huhuhudia/lobster-go/internal/agent"
	agentctx "github.com/huhuhudia/lobster-go/internal/agent/context"
	"github.com/huhuhudia/lobster-go/internal/agent/tools"
	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/memory"
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

type e2eProvider struct{}

func (p *e2eProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	hasToolResult := false
	for _, m := range req.Messages {
		if m.Role == "tool" {
			hasToolResult = true
			break
		}
	}
	if hasToolResult {
		return providers.ChatResponse{
			Message: providers.ChatMessage{Role: "assistant", Content: "done-e2e"},
		}, nil
	}
	return providers.ChatResponse{
		Message: providers.ChatMessage{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{
					ID:   "1",
					Name: "write_file",
					Arguments: map[string]interface{}{
						"path":    "note.txt",
						"content": "written-by-tool",
					},
				},
			},
		},
		HasToolCall: true,
	}, nil
}

func (p *e2eProvider) DefaultModel() string { return "mock-e2e" }

func TestAgentFlowE2E(t *testing.T) {
	workspace := t.TempDir()
	b := bus.New(10)
	smgr := session.NewManager(filepath.Join(workspace, "sessions"))
	prov := &e2eProvider{}

	lp := agent.NewLoop(
		b,
		prov,
		smgr,
		agentctx.Builder{SystemPrompt: "test-e2e"},
		agent.LoopConfig{
			Workspace:           workspace,
			RestrictToWorkspace: true,
			ExecTimeoutSec:      2,
			MaxIterations:       5,
		},
	)
	lp.RegisterTool(tools.WriteFileTool{Workspace: workspace, Restrict: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go lp.Run(ctx)

	if err := b.PublishInbound(bus.InboundMessage{
		Channel: "mock",
		ChatID:  "chat-1",
		Content: "please create a note",
	}); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}

	outCtx, outCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer outCancel()
	out, err := b.ConsumeOutbound(outCtx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if out.Content != "done-e2e" {
		t.Fatalf("unexpected outbound content: %s", out.Content)
	}

	toolOutput, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatalf("read tool output file: %v", err)
	}
	if string(toolOutput) != "written-by-tool" {
		t.Fatalf("unexpected tool file content: %s", string(toolOutput))
	}

	var sess session.Session
	found := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		smgr.Invalidate("mock:chat-1")
		var loadErr error
		sess, loadErr = smgr.GetOrCreate("mock:chat-1")
		if loadErr == nil && len(sess.Messages) >= 3 {
			found = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected at least user/tool/assistant messages, got %d", len(sess.Messages))
	}

	store := memory.NewStore(workspace)
	if err := store.Consolidate("assistant replied: "+out.Content, "remember-e2e"); err != nil {
		t.Fatalf("memory consolidate: %v", err)
	}
	if store.ReadMemory() != "remember-e2e" {
		t.Fatalf("memory content mismatch")
	}
	historyBytes, err := os.ReadFile(filepath.Join(workspace, "memory", "HISTORY.md"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(string(historyBytes), "assistant replied: done-e2e") {
		t.Fatalf("history missing expected summary")
	}
}
