package context

import (
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

// Builder is a simple context builder composing system prompt and history.
type Builder struct {
	SystemPrompt string
}

func (b Builder) Build(sess session.Session, tools []providers.ToolDefinition) []providers.ChatMessage {
	msgs := []providers.ChatMessage{}
	if b.SystemPrompt != "" {
		msgs = append(msgs, providers.ChatMessage{
			Role:    "system",
			Content: b.SystemPrompt,
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
