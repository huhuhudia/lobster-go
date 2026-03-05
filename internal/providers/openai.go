package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// OpenAIProvider implements a minimal chat completions client (no streaming).
type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
	Adapter ToolCallAdapter
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if p.APIKey == "" {
		return ChatResponse{}, errors.New("openai apiKey is required")
	}
	model := req.Model
	if model == "" {
		model = p.DefaultModel()
	}
	model = sanitizeModel(model)
	body := map[string]interface{}{
		"model":       model,
		"messages":    toOpenAIMessages(req.Messages),
		"temperature": req.Temperature,
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}

	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	requestURL := normalizeChatCompletionsURL(p.BaseURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai build request failed: url=%s err=%w", requestURL, err)
	}
	request.Header.Set("Authorization", "Bearer "+p.APIKey)
	request.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai request failed: url=%s err=%w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		bodyText := strings.TrimSpace(string(body))
		if bodyText == "" {
			return ChatResponse{}, fmt.Errorf("openai status %d url=%s", resp.StatusCode, requestURL)
		}
		return ChatResponse{}, fmt.Errorf("openai status %d url=%s body=%s", resp.StatusCode, requestURL, bodyText)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Role       string      `json:"role"`
				Content    interface{} `json:"content"`
				ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
				ToolCallID string      `json:"tool_call_id,omitempty"`
				Name       string      `json:"name,omitempty"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, err
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, errors.New("empty response")
	}
	msg := parsed.Choices[0].Message
	p.adapter().Normalize(msg.ToolCalls)
	return ChatResponse{
		Message: ChatMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		},
		HasToolCall: len(msg.ToolCalls) > 0,
		Usage:       parsed.Usage,
	}, nil
}

func (p *OpenAIProvider) DefaultModel() string {
	if p.Model != "" {
		return p.Model
	}
	return "gpt-4.1"
}

// NewOpenAIProvider constructs provider with defaults.
func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	baseURL = normalizeChatCompletionsURL(baseURL)
	if model == "" {
		model = "gpt-4.1"
	}
	return &OpenAIProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		Client:  &http.Client{Timeout: 20 * time.Second},
		Adapter: OpenAIAdapter{},
	}
}

// toOpenAIMessages converts internal messages to OpenAI format.
func toOpenAIMessages(msgs []ChatMessage) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		entry := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.Name != "" {
			entry["name"] = m.Name
		}
		if len(m.ToolCalls) > 0 {
			entry["tool_calls"] = (&OpenAIAdapter{}).ToWire(m.ToolCalls)
		}
		if m.ToolCallID != "" {
			entry["tool_call_id"] = m.ToolCallID
		}
		out = append(out, entry)
	}
	return out
}

// sanitizeModel trims whitespace from model string.
func sanitizeModel(model string) string {
	return strings.TrimSpace(model)
}

func (p *OpenAIProvider) adapter() ToolCallAdapter {
	if p.Adapter == nil {
		return OpenAIAdapter{}
	}
	return p.Adapter
}

func normalizeChatCompletionsURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "https://api.openai.com/v1/chat/completions"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return baseURL
	}

	cleanPath := strings.TrimSuffix(u.Path, "/")
	if cleanPath == "" {
		u.Path = "/v1/chat/completions"
		return u.String()
	}
	if cleanPath == "/v1" || strings.HasSuffix(cleanPath, "/v1") {
		u.Path = path.Join(cleanPath, "chat/completions")
		if !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}
		return u.String()
	}
	u.Path = cleanPath
	return u.String()
}
