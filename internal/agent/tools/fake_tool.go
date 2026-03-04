package tools

import (
	"context"

	"github.com/huhuhudia/lobster-go/internal/providers"
)

// FakeTool echoes a preset result; useful for tests.
type FakeTool struct {
	Result string
	Err    error
}

func (f FakeTool) Name() string { return "fake" }

func (f FakeTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        f.Name(),
			"description": "fake tool for tests",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (f FakeTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	return f.Result, nil
}
