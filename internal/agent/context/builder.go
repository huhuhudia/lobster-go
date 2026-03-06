package context

import (
	"strings"

	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
	"github.com/huhuhudia/lobster-go/internal/skills"
)

// Builder is a simple context builder composing system prompt and history.
type Builder struct {
	SystemPrompt string
	Workspace    string
	Skills       *skills.Loader
}

func (b Builder) Build(sess session.Session, tools []providers.ToolDefinition) []providers.ChatMessage {
	msgs := []providers.ChatMessage{}
	systemPrompt := b.buildSystemPrompt()
	if systemPrompt != "" {
		msgs = append(msgs, providers.ChatMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	for _, m := range sess.Messages {
		var calls []providers.ToolCall
		if tc, ok := m.ToolCalls.([]providers.ToolCall); ok {
			calls = tc
		}
		msgs = append(msgs, providers.ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCalls:  calls,
			ToolCallID: m.ToolCallID,
		})
	}
	return msgs
}

func (b Builder) BuildToolResult(content string) providers.ChatMessage {
	return providers.ChatMessage{Role: "tool", Content: content}
}

func (b Builder) buildSystemPrompt() string {
	parts := []string{}
	if strings.TrimSpace(b.SystemPrompt) != "" {
		parts = append(parts, b.SystemPrompt)
	}
	if strings.TrimSpace(b.Workspace) == "" {
		return strings.Join(parts, "\n\n---\n\n")
	}
	loader := b.Skills
	if loader == nil {
		loader = skills.NewLoader(b.Workspace, "")
	}
	alwaysSkills := loader.GetAlwaysSkills()
	if len(alwaysSkills) > 0 {
		content := loader.LoadSkillsForContext(alwaysSkills)
		if strings.TrimSpace(content) != "" {
			parts = append(parts, "# Active Skills\n\n"+content)
		}
	}
	summary := loader.BuildSkillsSummary()
	if strings.TrimSpace(summary) != "" {
		parts = append(parts, "# Skills\n\nThe following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.\nSkills with available=\"false\" need dependencies installed first.\n\n"+summary)
	}
	return strings.Join(parts, "\n\n---\n\n")
}
