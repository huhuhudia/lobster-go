package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/internal/session"
)

// Store manages MEMORY.md and HISTORY.md under workspace/memory.
type Store struct {
	Dir         string
	MemoryFile  string
	HistoryFile string
}

// ConsolidateMode controls how messages are selected for consolidation.
type ConsolidateMode string

const (
	ConsolidateModeWindow     ConsolidateMode = "window"
	ConsolidateModeArchiveAll ConsolidateMode = "archive_all"
)

// ConsolidateOptions configures provider-driven memory consolidation.
type ConsolidateOptions struct {
	Mode        ConsolidateMode
	WindowSize  int
	Model       string
	MaxTokens   int
	Temperature float64
}

// ConsolidateResult describes what range was processed.
type ConsolidateResult struct {
	Summary       string
	MemoryUpdate  string
	ProcessedFrom int
	ProcessedTo   int
	Updated       bool
}

// NewStore creates a store under workspace/memory.
func NewStore(workspace string) *Store {
	dir := filepath.Join(workspace, "memory")
	return &Store{
		Dir:         dir,
		MemoryFile:  filepath.Join(dir, "MEMORY.md"),
		HistoryFile: filepath.Join(dir, "HISTORY.md"),
	}
}

// ReadMemory returns MEMORY.md content (empty if missing).
func (s *Store) ReadMemory() string {
	data, err := os.ReadFile(s.MemoryFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteMemory overwrites MEMORY.md.
func (s *Store) WriteMemory(content string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.MemoryFile, []byte(content), 0o644)
}

// AppendHistory appends to HISTORY.md with blank line separation.
func (s *Store) AppendHistory(entry string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.HistoryFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte(entry + "\n\n")); err != nil {
		return err
	}
	return nil
}

// Consolidate is a stub for LLM-driven consolidation; currently copies latest session text.
func (s *Store) Consolidate(summary string, memoryUpdate string) error {
	if memoryUpdate != "" {
		if err := s.WriteMemory(memoryUpdate); err != nil {
			return err
		}
	}
	if summary != "" {
		if err := s.AppendHistory(summary); err != nil {
			return err
		}
	}
	return nil
}

// ConsolidateSession uses a provider to summarize session messages and update MEMORY/HISTORY files.
// It updates sess.LastConsolidated when processing succeeds.
func (s *Store) ConsolidateSession(ctx context.Context, provider providers.Provider, sess *session.Session, opts ConsolidateOptions) (ConsolidateResult, error) {
	if provider == nil {
		return ConsolidateResult{}, fmt.Errorf("provider is required")
	}
	if sess == nil {
		return ConsolidateResult{}, fmt.Errorf("session is required")
	}

	mode := opts.Mode
	if mode == "" {
		mode = ConsolidateModeWindow
	}
	windowSize := opts.WindowSize
	if windowSize <= 0 {
		windowSize = 50
	}

	start, end := selectRange(sess, mode, windowSize)
	if start >= end {
		return ConsolidateResult{
			ProcessedFrom: start,
			ProcessedTo:   end,
			Updated:       false,
		}, nil
	}

	req := providers.ChatRequest{
		Model:       opts.Model,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		Tools:       []providers.ToolDefinition{saveMemoryToolDefinition()},
		Messages: []providers.ChatMessage{
			{
				Role:    "system",
				Content: "Consolidate conversation memory. Always respond with the save_memory tool call.",
			},
			{
				Role: "user",
				Content: buildConsolidationPrompt(
					s.ReadMemory(),
					sess.Messages[start:end],
					start,
				),
			},
		},
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		return ConsolidateResult{}, err
	}

	summary, memoryUpdate, err := parseSaveMemory(resp)
	if err != nil {
		return ConsolidateResult{}, err
	}

	if err := s.Consolidate(summary, memoryUpdate); err != nil {
		return ConsolidateResult{}, err
	}

	sess.LastConsolidated = end
	return ConsolidateResult{
		Summary:       summary,
		MemoryUpdate:  memoryUpdate,
		ProcessedFrom: start,
		ProcessedTo:   end,
		Updated:       summary != "" || memoryUpdate != "",
	}, nil
}

func selectRange(sess *session.Session, mode ConsolidateMode, windowSize int) (int, int) {
	total := len(sess.Messages)
	if total == 0 {
		return 0, 0
	}

	if mode == ConsolidateModeArchiveAll {
		return 0, total
	}

	start := sess.LastConsolidated
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := total
	if end-start > windowSize {
		end = start + windowSize
	}
	return start, end
}

func saveMemoryToolDefinition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        "save_memory",
			"description": "Persist memory updates from recent conversation",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"summary": map[string]interface{}{
						"type":        "string",
						"description": "Summary to append into HISTORY.md",
					},
					"memory_update": map[string]interface{}{
						"type":        "string",
						"description": "New content for MEMORY.md",
					},
				},
				"required": []string{"summary", "memory_update"},
			},
		},
	}
}

func buildConsolidationPrompt(existingMemory string, msgs []session.Message, startIndex int) string {
	var b strings.Builder
	b.WriteString("Current MEMORY.md:\n")
	if strings.TrimSpace(existingMemory) == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(existingMemory)
		if !strings.HasSuffix(existingMemory, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("\nConversation slice:\n")
	for i, msg := range msgs {
		content := stringify(msg.Content)
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", startIndex+i, strings.ToLower(msg.Role), content))
	}
	b.WriteString("\nReturn save_memory(summary, memory_update).")
	return b.String()
}

func parseSaveMemory(resp providers.ChatResponse) (string, string, error) {
	if len(resp.Message.ToolCalls) > 0 {
		tc := resp.Message.ToolCalls[0]
		if tc.Name != "save_memory" {
			return "", "", fmt.Errorf("unexpected tool call: %s", tc.Name)
		}
		return readToolArgs(tc.Arguments), readToolMemory(tc.Arguments), nil
	}
	content := strings.TrimSpace(stringify(resp.Message.Content))
	if content == "" {
		return "", "", fmt.Errorf("provider did not return save_memory tool call")
	}
	return content, "", nil
}

func readToolArgs(args map[string]interface{}) string {
	if v, ok := args["summary"]; ok {
		return strings.TrimSpace(stringify(v))
	}
	return ""
}

func readToolMemory(args map[string]interface{}) string {
	if v, ok := args["memory_update"]; ok {
		return strings.TrimSpace(stringify(v))
	}
	return ""
}

func stringify(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

// HistoryEntry returns a formatted history line with timestamp (helper for callers).
func HistoryEntry(summary string) string {
	return fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), strings.TrimSpace(summary))
}
