package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockProviderSuccess(t *testing.T) {
	mp := &MockProvider{
		Response: ChatResponse{
			Message: ChatMessage{Role: "assistant", Content: "ok"},
		},
	}
	resp, err := mp.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content mismatch: %v", resp.Message.Content)
	}
}

func TestMockProviderError(t *testing.T) {
	mp := &MockProvider{Err: errors.New("boom")}
	if _, err := mp.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMockProviderTimeout(t *testing.T) {
	mp := &MockProvider{Delay: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := mp.Chat(ctx, ChatRequest{}); err == nil {
		t.Fatalf("expected timeout error")
	}
}
