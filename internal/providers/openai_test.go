package providers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
					},
				},
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
}
