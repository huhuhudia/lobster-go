package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed *.md
var templateFS embed.FS

// Template files embedded in binary
var defaultTemplates = map[string]string{
	"AGENTS.md": `# Agent Instructions

You are a helpful AI assistant. Be concise, accurate, and friendly.

## Scheduled Reminders

Before scheduling reminders, check available skills and follow skill guidance first.
Use the built-in cron tool to create/list/remove jobs.

## Heartbeat Tasks

HEARTBEAT.md is checked on the configured heartbeat interval. Use file tools to manage periodic tasks:

- **Add**: edit_file to append new tasks
- **Remove**: edit_file to delete completed tasks
- **Rewrite**: write_file to replace all tasks
`,
	"TOOLS.md": `# Tool Usage Notes

Tool signatures are provided automatically via function calling.
This file documents non-obvious constraints and usage patterns.

## exec — Safety Limits

- Commands have a configurable timeout (default 60s)
- Dangerous commands are blocked (rm -rf, format, dd, shutdown, etc.)
- Output is truncated at 10,000 characters
- restrictToWorkspace config can limit file access to the workspace

## cron — Scheduled Reminders

Use the cron tool to schedule reminders and recurring tasks.
`,
	"USER.md": `# User Profile

Information about the user to help personalize interactions.

## Basic Information

- **Name**: (your name)
- **Timezone**: (your timezone, e.g., UTC+8)
- **Language**: (preferred language)

## Preferences

### Communication Style

- [ ] Casual
- [ ] Professional
- [ ] Technical

### Response Length

- [ ] Brief and concise
- [ ] Detailed explanations
- [ ] Adaptive based on question

---

*Edit this file to customize lobster-go's behavior for your needs.*
`,
	"SOUL.md": `# Soul

I am lobster-go, a personal AI assistant.

## Personality

- Helpful and friendly
- Concise and to the point
- Curious and eager to learn

## Values

- Accuracy over speed
- User privacy and safety
- Transparency in actions

## Communication Style

- Be clear and direct
- Explain reasoning when helpful
- Ask clarifying questions when needed
`,
	"HEARTBEAT.md": `# Heartbeat Tasks

This file is checked on the configured heartbeat interval.
Add periodic tasks here for the agent to execute.

## Current Tasks

(None yet — add tasks below)

`,
}

// Memory templates
var memoryTemplates = map[string]string{
	"MEMORY.md": `# Auto Memory

This file stores persistent memories. Important facts about the user and context are saved here.

## User Preferences

-

## Important Context

-

`,
	"HISTORY.md": `# History Log

This is an append-only log of events. Search it with grep.

`,
}

// Sync synchronizes templates to the workspace directory.
// Only creates files that don't exist.
func Sync(workspace string) ([]string, error) {
	var added []string

	// Sync main templates
	for name, content := range defaultTemplates {
		path := filepath.Join(workspace, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return added, fmt.Errorf("write %s: %w", name, err)
			}
			added = append(added, name)
		}
	}

	// Create memory directory
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return added, fmt.Errorf("create memory dir: %w", err)
	}

	// Sync memory templates
	for name, content := range memoryTemplates {
		path := filepath.Join(memoryDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return added, fmt.Errorf("write memory/%s: %w", name, err)
			}
			added = append(added, "memory/"+name)
		}
	}

	// Create skills directory
	skillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return added, fmt.Errorf("create skills dir: %w", err)
	}

	// Create history directory
	historyDir := filepath.Join(workspace, "history")
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		return added, fmt.Errorf("create history dir: %w", err)
	}

	return added, nil
}

// GetTemplate returns a template by name, or empty string if not found.
func GetTemplate(name string) string {
	if content, ok := defaultTemplates[name]; ok {
		return content
	}
	if content, ok := memoryTemplates[name]; ok {
		return content
	}
	return ""
}

// ListTemplates returns all available template names.
func ListTemplates() []string {
	var names []string
	for name := range defaultTemplates {
		names = append(names, name)
	}
	for name := range memoryTemplates {
		names = append(names, "memory/"+name)
	}
	return names
}