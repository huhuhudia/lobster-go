package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderListAndLoad(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := filepath.Join(workspace, "skills", "alpha")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	content := `---
description: Alpha skill
metadata: {"nanobot":{"requires":{"bins":["nonexistent-bin"]}}}
---

alpha`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	loader := NewLoader(workspace, "")
	all := loader.ListSkills(false)
	if len(all) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(all))
	}
	if all[0].Name != "alpha" || all[0].Source != "workspace" {
		t.Fatalf("unexpected skill info: %+v", all[0])
	}
	if all[0].Available {
		t.Fatalf("expected skill to be unavailable due to missing bin")
	}
	if !strings.Contains(all[0].Missing, "CLI: nonexistent-bin") {
		t.Fatalf("missing requirements not reported")
	}

	available := loader.ListSkills(true)
	if len(available) != 0 {
		t.Fatalf("expected unavailable skills to be filtered")
	}

	loaded, ok := loader.LoadSkill("alpha")
	if !ok || !strings.Contains(loaded, "alpha") {
		t.Fatalf("load skill failed")
	}
}

func TestLoadSkillsForContextAndAlways(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := filepath.Join(workspace, "skills", "beta")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	content := `---
description: Beta skill
metadata: {"nanobot":{"always":true}}
---

beta`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	loader := NewLoader(workspace, "")
	always := loader.GetAlwaysSkills()
	if len(always) != 1 || always[0] != "beta" {
		t.Fatalf("expected beta as always skill")
	}
	ctx := loader.LoadSkillsForContext(always)
	if !strings.Contains(ctx, "### Skill: beta") || !strings.Contains(ctx, "beta") {
		t.Fatalf("context not formatted correctly")
	}
	summary := loader.BuildSkillsSummary()
	if !strings.Contains(summary, "<skills>") || !strings.Contains(summary, "beta") {
		t.Fatalf("summary missing expected skill")
	}
}
