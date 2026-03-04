package providers

import (
	"context"
	"time"
)

// ToolCall models an OpenAI-style tool call.
type ToolCall struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ChatMessage represents a single message in the provider API.
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
	// Optional fields
	Name      string      `json:"name,omitempty"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatRequest is the input to a provider chat call.
type ChatRequest struct {
	Messages       []ChatMessage
	Tools          []ToolDefinition
	Model          string
	Temperature    float64
	MaxTokens      int
	ReasoningEffort string
	Timeout        time.Duration
}

// ToolDefinition describes a callable tool/function.
type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function map[string]interface{} `json:"function"`
}

// ChatResponse wraps provider output.
type ChatResponse struct {
	Message     ChatMessage
	HasToolCall bool
}

// Provider is the interface implemented by LLM backends.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	DefaultModel() string
}
