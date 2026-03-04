package memory

import "testing"

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
