package memory

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

// Store manages MEMORY.md and HISTORY.md under workspace/memory.
type Store struct {
	Dir        string
	MemoryFile string
	HistoryFile string
}

// NewStore creates a store under workspace/memory.
func NewStore(workspace string) *Store {
	dir := filepath.Join(workspace, "memory")
	return &Store{
		Dir:         dir,
		MemoryFile:  filepath.Join(dir, "MEMORY.md"),
		HistoryFile: filepath.Join(dir, "HISTORY.md"),
	}
}

// ReadMemory returns MEMORY.md content (empty if missing).
func (s *Store) ReadMemory() string {
	data, err := ioutil.ReadFile(s.MemoryFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteMemory overwrites MEMORY.md.
func (s *Store) WriteMemory(content string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return ioutil.WriteFile(s.MemoryFile, []byte(content), 0o644)
}

// AppendHistory appends to HISTORY.md with blank line separation.
func (s *Store) AppendHistory(entry string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.HistoryFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte(entry + "\n\n")); err != nil {
		return err
	}
	return nil
}

// Consolidate is a stub for LLM-driven consolidation; currently copies latest session text.
func (s *Store) Consolidate(summary string, memoryUpdate string) error {
	if memoryUpdate != "" {
		if err := s.WriteMemory(memoryUpdate); err != nil {
			return err
		}
	}
	if summary != "" {
		if err := s.AppendHistory(summary); err != nil {
			return err
		}
	}
	return nil
}
