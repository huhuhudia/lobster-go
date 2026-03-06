package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huhuhudia/lobster-go/internal/session"
)

func TestBuilderAddsSystemAndHistory(t *testing.T) {
	s := session.New("k")
	s.AddMessage("user", "hi")
	b := Builder{SystemPrompt: "you are tester"}
	msgs := b.Build(s, nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("role order mismatch")
	}
}

func TestBuilderAddsSkillsSummary(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := filepath.Join(workspace, "skills", "alpha")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	skill := `---
description: Alpha skill
metadata: {"nanobot":{"always":true}}
---

Use this skill to do alpha tasks.
`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	s := session.New("k")
	s.AddMessage("user", "hi")
	b := Builder{SystemPrompt: "base", Workspace: workspace}
	msgs := b.Build(s, nil)
	if len(msgs) < 2 {
		t.Fatalf("expected system + user messages")
	}
	if !strings.Contains(msgs[0].Content.(string), "<skills>") {
		t.Fatalf("expected skills summary in system prompt")
	}
	if !strings.Contains(msgs[0].Content.(string), "Alpha skill") {
		t.Fatalf("expected skill description in system prompt")
	}
	if !strings.Contains(msgs[0].Content.(string), "Active Skills") {
		t.Fatalf("expected always skill content in system prompt")
	}
}
