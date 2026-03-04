package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/huhuhudia/lobster-go/internal/providers"
)

// WebFetchTool fetches a URL via HTTP GET (simplified).
type WebFetchTool struct {
	TimeoutSec int
}

func (t WebFetchTool) Name() string { return "web_fetch" }

func (t WebFetchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "Fetch a URL with HTTP GET",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to fetch (http/https)",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (t WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	raw, ok := args["url"].(string)
	if !ok || raw == "" {
		return "", errors.New("url required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("invalid url")
	}
	timeout := time.Duration(t.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// WebSearchTool is a placeholder that echoes the query (no external API here).
type WebSearchTool struct{}

func (t WebSearchTool) Name() string { return "web_search" }

func (t WebSearchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "Search the web and return synthetic results (stub)",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query string",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (t WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	q, _ := args["query"].(string)
	if strings.TrimSpace(q) == "" {
		return "", errors.New("query required")
	}
	// Stub: return echoed query until integrated with real API.
	return "results for: " + q, nil
}
