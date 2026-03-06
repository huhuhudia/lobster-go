package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// OpenAIProvider implements a minimal chat completions client (streaming supported).
type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
	Adapter ToolCallAdapter
}

const defaultRequestTimeout = 120 * time.Second

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return p.chat(ctx, req, true)
}

func (p *OpenAIProvider) chat(ctx context.Context, req ChatRequest, useStream bool) (ChatResponse, error) {
	if p.APIKey == "" {
		return ChatResponse{}, errors.New("openai apiKey is required")
	}
	effectiveTimeout := req.Timeout
	if effectiveTimeout <= 0 {
		effectiveTimeout = defaultRequestTimeout
	}
	if effectiveTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, effectiveTimeout)
		defer cancel()
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
	if strings.TrimSpace(req.PromptCacheKey) != "" {
		body["prompt_cache_key"] = req.PromptCacheKey
	}
	if strings.TrimSpace(req.PromptCacheRetention) != "" {
		body["prompt_cache_retention"] = req.PromptCacheRetention
	}
	if useStream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}

	client := p.Client
	if client == nil {
		client = &http.Client{}
	}
	localClient := *client
	// Avoid client-side read timeout; rely on ctx timeout instead.
	localClient.Timeout = 0

	requestURL := normalizeChatCompletionsURL(p.BaseURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai build request failed: url=%s err=%w", requestURL, err)
	}
	request.Header.Set("Authorization", "Bearer "+p.APIKey)
	request.Header.Set("Content-Type", "application/json")

	resp, err := localClient.Do(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai request failed: url=%s err=%w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		bodyText := strings.TrimSpace(string(body))
		if useStream && shouldRetryWithoutStream(resp.StatusCode, bodyText) {
			return p.chat(ctx, req, false)
		}
		if bodyText == "" {
			return ChatResponse{}, fmt.Errorf("openai status %d url=%s", resp.StatusCode, requestURL)
		}
		return ChatResponse{}, fmt.Errorf("openai status %d url=%s body=%s", resp.StatusCode, requestURL, bodyText)
	}

	if useStream && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return p.parseStreamResponse(resp.Body)
	}
	return p.parseJSONResponse(resp.Body)
}

func (p *OpenAIProvider) parseJSONResponse(body io.Reader) (ChatResponse, error) {
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
		Usage *usagePayload `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return ChatResponse{}, err
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, errors.New("empty response")
	}
	msg := parsed.Choices[0].Message
	p.adapter().Normalize(msg.ToolCalls)
	usage := convertUsage(parsed.Usage)
	return ChatResponse{
		Message: ChatMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		},
		HasToolCall: len(msg.ToolCalls) > 0,
		Usage:       usage,
	}, nil
}

func (p *OpenAIProvider) parseStreamResponse(body io.Reader) (ChatResponse, error) {
	type streamToolFn struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	}
	type streamToolCall struct {
		Index    int          `json:"index"`
		ID       string       `json:"id,omitempty"`
		Type     string       `json:"type,omitempty"`
		Function streamToolFn `json:"function,omitempty"`
	}
	type streamDelta struct {
		Role      string           `json:"role,omitempty"`
		Content   string           `json:"content,omitempty"`
		ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
	}
	type streamChoice struct {
		Delta        streamDelta `json:"delta"`
		FinishReason string      `json:"finish_reason,omitempty"`
	}
	type streamChunk struct {
		Choices []streamChoice `json:"choices"`
		Usage   *usagePayload  `json:"usage,omitempty"`
	}
	type toolAcc struct {
		ID        string
		Type      string
		Name      string
		Arguments string
	}

	reader := bufio.NewReader(body)
	var content strings.Builder
	role := ""
	toolCalls := map[int]*toolAcc{}
	var usage *Usage

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return ChatResponse{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = convertUsage(chunk.Usage)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Role != "" {
			role = delta.Role
		}
		if delta.Content != "" {
			content.WriteString(delta.Content)
		}
		for _, tc := range delta.ToolCalls {
			acc := toolCalls[tc.Index]
			if acc == nil {
				acc = &toolAcc{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.Arguments += tc.Function.Arguments
			}
		}
	}

	if role == "" {
		role = "assistant"
	}
	assembled := []ToolCall{}
	if len(toolCalls) > 0 {
		indexes := make([]int, 0, len(toolCalls))
		for idx := range toolCalls {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			acc := toolCalls[idx]
			if acc == nil {
				continue
			}
			call := ToolCall{
				ID:   acc.ID,
				Type: acc.Type,
				Function: &ToolFunction{
					Name:      acc.Name,
					Arguments: acc.Arguments,
				},
			}
			assembled = append(assembled, call)
		}
	}
	p.adapter().Normalize(assembled)

	msg := ChatMessage{
		Role:      role,
		Content:   content.String(),
		ToolCalls: assembled,
	}
	return ChatResponse{
		Message:     msg,
		HasToolCall: len(assembled) > 0,
		Usage:       usage,
	}, nil
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	PromptDetails    *struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
}

func shouldRetryWithoutStream(status int, bodyText string) bool {
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		low := strings.ToLower(bodyText)
		if strings.Contains(low, "stream") || strings.Contains(low, "streaming") {
			return true
		}
	}
	return false
}

func convertUsage(u *usagePayload) *Usage {
	if u == nil {
		return nil
	}
	usage := &Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
	if u.PromptDetails != nil {
		usage.CachedTokens = u.PromptDetails.CachedTokens
	}
	return usage
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
		Client:  &http.Client{},
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
