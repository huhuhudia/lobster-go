package providers

import (
	"errors"

	"github.com/huhuhudia/lobster-go/internal/config"
)

// BuildProvider chooses a provider based on config; defaults to MockProvider.
func BuildProvider(cfg config.Config) Provider {
	// prefer openai if configured
	if openai, ok := cfg.Providers["openai"]; ok {
		if openai.APIKey != "" {
			return NewOpenAIProvider(openai.APIKey, openai.BaseURL, openai.Model)
		}
	}
	return &MockProvider{
		Response: ChatResponse{
			Message: ChatMessage{Role: "assistant", Content: "pong"},
		},
	}
}

// ErrUnsupportedProvider is returned when requested provider missing.
var ErrUnsupportedProvider = errors.New("unsupported provider")
