package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

type captureProvider struct {
	resp    providers.ChatResponse
	calls   int
	lastReq providers.ChatRequest
}

func (p *captureProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	p.calls++
	p.lastReq = req
	return p.resp, nil
}

func (p *captureProvider) DefaultModel() string { return "mock-memory" }

func TestStoreReadWrite(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.WriteMemory("hi"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := s.ReadMemory(); got != "hi" {
		t.Fatalf("read mismatch: %s", got)
	}
	if err := s.AppendHistory("event"); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestConsolidate(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Consolidate("summary", "memory"); err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if s.ReadMemory() != "memory" {
		t.Fatalf("memory not updated")
	}
}

func TestConsolidateSessionWindowMode(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	if err := store.WriteMemory("old-memory"); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	sess := session.New("mock:1")
	for i := 0; i < 6; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		sess.AddMessage(role, "msg")
	}

	prov := &captureProvider{
		resp: providers.ChatResponse{
			Message: providers.ChatMessage{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{
						ID:   "1",
						Name: "save_memory",
						Arguments: map[string]interface{}{
							"summary":       "window-summary",
							"memory_update": "new-memory-window",
						},
					},
				},
			},
			HasToolCall: true,
		},
	}

	result, err := store.ConsolidateSession(context.Background(), prov, &sess, ConsolidateOptions{
		Mode:       ConsolidateModeWindow,
		WindowSize: 4,
	})
	if err != nil {
		t.Fatalf("consolidate session: %v", err)
	}
	if result.ProcessedFrom != 0 || result.ProcessedTo != 4 {
		t.Fatalf("unexpected processed range: %+v", result)
	}
	if sess.LastConsolidated != 4 {
		t.Fatalf("last_consolidated mismatch: %d", sess.LastConsolidated)
	}
	if store.ReadMemory() != "new-memory-window" {
		t.Fatalf("memory not updated")
	}
	history, err := os.ReadFile(filepath.Join(workspace, "memory", "HISTORY.md"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(string(history), "window-summary") {
		t.Fatalf("history missing summary")
	}
	if prov.calls != 1 {
		t.Fatalf("expected provider call count 1, got %d", prov.calls)
	}
	if len(prov.lastReq.Tools) != 1 {
		t.Fatalf("expected save_memory tool in request")
	}
}

func TestConsolidateSessionArchiveAll(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)

	sess := session.New("mock:2")
	sess.AddMessage("user", "a")
	sess.AddMessage("assistant", "b")
	sess.AddMessage("user", "c")
	sess.LastConsolidated = 2

	prov := &captureProvider{
		resp: providers.ChatResponse{
			Message: providers.ChatMessage{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{
						ID:   "1",
						Name: "save_memory",
						Arguments: map[string]interface{}{
							"summary":       "archive-all-summary",
							"memory_update": "archive-all-memory",
						},
					},
				},
			},
			HasToolCall: true,
		},
	}

	result, err := store.ConsolidateSession(context.Background(), prov, &sess, ConsolidateOptions{
		Mode: ConsolidateModeArchiveAll,
	})
	if err != nil {
		t.Fatalf("consolidate session: %v", err)
	}
	if result.ProcessedFrom != 0 || result.ProcessedTo != 3 {
		t.Fatalf("unexpected processed range: %+v", result)
	}
	if sess.LastConsolidated != 3 {
		t.Fatalf("last_consolidated mismatch: %d", sess.LastConsolidated)
	}
}

func TestConsolidateSessionNoNewMessages(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := session.New("mock:3")
	sess.AddMessage("user", "a")
	sess.LastConsolidated = 1

	prov := &captureProvider{
		resp: providers.ChatResponse{
			Message: providers.ChatMessage{Role: "assistant", Content: "unused"},
		},
	}

	result, err := store.ConsolidateSession(context.Background(), prov, &sess, ConsolidateOptions{
		Mode: ConsolidateModeWindow,
	})
	if err != nil {
		t.Fatalf("consolidate session: %v", err)
	}
	if result.Updated {
		t.Fatalf("expected no update")
	}
	if prov.calls != 0 {
		t.Fatalf("expected provider to not be called")
	}
}
