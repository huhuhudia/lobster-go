package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndHistory(t *testing.T) {
	s := New("test")
	s.AddMessage("system", "hello")
	s.AddMessage("assistant", "reply")
	s.AddMessage("user", "ask")
	h := s.GetHistory(10)
	if len(h) != 1 {
		t.Fatalf("history len mismatch: %d", len(h))
	}
	if h[0].Role != "user" || h[0].Content != "ask" {
		t.Fatalf("expected first user message only, got %+v", h[0])
	}
	// ensure leading non-user drop
	s.LastConsolidated = 0
	h2 := s.GetHistory(2)
	if len(h2) != 1 || h2[0].Role != "user" {
		t.Fatalf("unexpected history after windowing: %+v", h2)
	}
}

func TestManagerSaveLoad(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	s := New("tg:1")
	s.AddMessage("user", "hi")
	if err := m.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := m.GetOrCreate("tg:1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(loaded.Messages))
	}
}

func TestManagerList(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	s := New("a:b")
	s.AddMessage("user", "x")
	if err := m.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	list, err := m.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list size mismatch: %d", len(list))
	}
}

func TestSafeFilename(t *testing.T) {
	name := safeFilename("tg:1/2\\3")
	if name == "" || name == "tg:1/2\\3" {
		t.Fatalf("safeFilename not sanitized: %s", name)
	}
}

func TestMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyDir := filepath.Join(os.TempDir(), "lobster-legacy-test")
	_ = os.MkdirAll(legacyDir, 0o755)
	legacyFile := filepath.Join(legacyDir, "tg_1.jsonl")
	data := `{"_type":"metadata","key":"tg:1","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","last_consolidated":0}
{"role":"user","content":"hi","timestamp":"2025-01-01T00:00:01Z"}` + "\n"
	if err := os.WriteFile(legacyFile, []byte(data), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	m := NewManager(dir)
	m.legacyDir = legacyDir
	s, err := m.GetOrCreate("tg:1")
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("expected migrated message, got %d", len(s.Messages))
	}
}
