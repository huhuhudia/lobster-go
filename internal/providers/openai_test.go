package providers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderParsesResponse(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("listen not permitted")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatalf("missing auth header")
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp := map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "hi",
						"tool_calls": []interface{}{
							map[string]interface{}{
								"id":   "call_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "list_dir",
									"arguments": "{\"path\":\".\"}",
								},
							},
						},
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     2,
				"completion_tokens": 3,
				"total_tokens":      5,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	server.Listener = ln
	server.Start()
	defer server.Close()

	p := NewOpenAIProvider("sk-test", server.URL, "gpt-test")
	out, err := p.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
	if out.Message.Content != "hi" {
		t.Fatalf("content mismatch: %v", out.Message.Content)
	}
	if out.Usage == nil || out.Usage.TotalTokens != 5 {
		t.Fatalf("usage not parsed: %+v", out.Usage)
	}
	if len(out.Message.ToolCalls) != 1 || out.Message.ToolCalls[0].Name != "list_dir" {
		t.Fatalf("tool_calls not normalized: %+v", out.Message.ToolCalls)
	}
}

func TestOpenAIAdapterToWire(t *testing.T) {
	adapter := OpenAIAdapter{}
	calls := []ToolCall{
		{
			ID:   "c1",
			Name: "read_file",
			Arguments: map[string]interface{}{
				"path": "a.txt",
			},
		},
	}
	wire := adapter.ToWire(calls)
	if len(wire) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(wire))
	}
	fn, ok := wire[0]["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function field")
	}
	if fn["name"] != "read_file" {
		t.Fatalf("unexpected function name: %v", fn["name"])
	}
	if _, ok := fn["arguments"].(string); !ok {
		t.Fatalf("expected arguments to be string")
	}
}

func TestNormalizeChatCompletionsURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://coding.dashscope.aliyuncs.com/v1", "https://coding.dashscope.aliyuncs.com/v1/chat/completions"},
		{"https://coding.dashscope.aliyuncs.com/v1/chat/completions", "https://coding.dashscope.aliyuncs.com/v1/chat/completions"},
	}
	for _, tt := range tests {
		if got := normalizeChatCompletionsURL(tt.in); got != tt.want {
			t.Fatalf("normalize(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOpenAIProviderErrorIncludesURL(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("listen not permitted")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	server.Listener = ln
	server.Start()
	defer server.Close()

	p := NewOpenAIProvider("sk-test", server.URL, "gpt-test")
	_, err = p.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "openai status 401") {
		t.Fatalf("unexpected error message: %s", errMsg)
	}
	if !strings.Contains(errMsg, "url="+server.URL+"/v1/chat/completions") {
		t.Fatalf("error message missing request url: %s", errMsg)
	}
}
