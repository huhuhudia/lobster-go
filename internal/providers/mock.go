package providers

import (
	"context"
	"errors"
	"time"
)

// MockProvider is a test double with programmable responses.
type MockProvider struct {
	Response ChatResponse
	Err      error
	Delay    time.Duration
}

func (m *MockProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if m.Delay > 0 {
		select {
		case <-ctx.Done():
			return ChatResponse{}, ctx.Err()
		case <-time.After(m.Delay):
		}
	}
	if m.Err != nil {
		return ChatResponse{}, m.Err
	}
	return m.Response, nil
}

func (m *MockProvider) DefaultModel() string {
	return "mock-model"
}

// ErrInvalidToolArgs is returned when tool arguments are malformed.
var ErrInvalidToolArgs = errors.New("invalid tool arguments")
