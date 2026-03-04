package context

import (
	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

// DummyBuilder is a minimal ContextBuilder used for tests.
type DummyBuilder struct{}

func (DummyBuilder) Build(sess session.Session, tools []providers.ToolDefinition) []providers.ChatMessage {
	msgs := make([]providers.ChatMessage, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, providers.ChatMessage{Role: m.Role, Content: m.Content})
	}
	return msgs
}

func (DummyBuilder) BuildToolResult(content string) providers.ChatMessage {
	return providers.ChatMessage{Role: "tool", Content: content}
}
