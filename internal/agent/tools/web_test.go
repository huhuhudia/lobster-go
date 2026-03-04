package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSearchStub(t *testing.T) {
	tool := WebSearchTool{}
	out, err := tool.Execute(context.Background(), map[string]interface{}{"query": "hello"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out == "" {
		t.Fatalf("expected stub output")
	}
}

func TestWebFetch(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("listen not permitted")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	server.Listener = ln
	server.Start()
	defer server.Close()

	tool := WebFetchTool{TimeoutSec: 2}
	out, err := tool.Execute(context.Background(), map[string]interface{}{"url": server.URL})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected body: %s", out)
	}
}
