package templates

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestSync(t *testing.T) {
	// Create temp workspace
	tmpDir := t.TempDir()

	added, err := Sync(tmpDir)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Check that templates were created
	if len(added) == 0 {
		t.Error("Expected some templates to be added")
	}

	// Verify main templates exist
	expectedFiles := []string{
		"AGENTS.md",
		"TOOLS.md",
		"USER.md",
		"SOUL.md",
		"HEARTBEAT.md",
	}

	for _, name := range expectedFiles {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", name)
		}
	}

	// Verify memory templates exist
	memoryFiles := []string{
		"memory/MEMORY.md",
		"memory/HISTORY.md",
	}

	for _, name := range memoryFiles {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", name)
		}
	}

	// Verify directories exist
	dirs := []string{"memory", "skills", "history"}
	for _, dir := range dirs {
		path := filepath.Join(tmpDir, dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected directory %s to exist", dir)
		}
	}
}

func TestSyncIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// First sync
	added1, err := Sync(tmpDir)
	if err != nil {
		t.Fatalf("First sync failed: %v", err)
	}

	// Second sync should not add any files
	added2, err := Sync(tmpDir)
	if err != nil {
		t.Fatalf("Second sync failed: %v", err)
	}

	if len(added2) != 0 {
		t.Errorf("Second sync should not add files, got: %v", added2)
	}

	// Check that first sync did add files
	if len(added1) == 0 {
		t.Error("First sync should have added files")
	}
}

func TestGetTemplate(t *testing.T) {
	tests := []struct {
		name     string
		wantEmpty bool
	}{
		{"AGENTS.md", false},
		{"TOOLS.md", false},
		{"USER.md", false},
		{"SOUL.md", false},
		{"HEARTBEAT.md", false},
		{"MEMORY.md", false},
		{"HISTORY.md", false},
		{"NONEXISTENT.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := GetTemplate(tt.name)
			if (content == "") != tt.wantEmpty {
				t.Errorf("GetTemplate(%q) empty=%v, want empty=%v", tt.name, content == "", tt.wantEmpty)
			}
		})
	}
}

func TestListTemplates(t *testing.T) {
	templates := ListTemplates()

	if len(templates) == 0 {
		t.Error("ListTemplates should return at least one template")
	}

	// Sort for consistent comparison
	sort.Strings(templates)

	// Check for expected templates
	expected := map[string]bool{
		"AGENTS.md":     true,
		"TOOLS.md":      true,
		"USER.md":       true,
		"SOUL.md":       true,
		"HEARTBEAT.md":  true,
		"memory/MEMORY.md":  true,
		"memory/HISTORY.md": true,
	}

	for _, name := range templates {
		if !expected[name] && !expected["memory/"+name] {
			// Allow memory templates without prefix
			found := false
			for exp := range expected {
				if name == exp || name == exp[7:] { // strip "memory/" prefix
					found = true
					break
				}
			}
			if !found {
				t.Logf("Unexpected template: %s", name)
			}
		}
	}
}