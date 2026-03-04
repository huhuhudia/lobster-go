package tools

import (
	"context"
	"testing"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

func TestMessageToolPublishes(t *testing.T) {
	b := bus.New(1)
	tool := MessageTool{Bus: b}
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"channel": "tg",
		"chat_id": "1",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out, err := b.ConsumeOutbound(context.Background())
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if out.Content != "hello" {
		t.Fatalf("content mismatch: %s", out.Content)
	}
}
