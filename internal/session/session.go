package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Message represents a stored message entry.
type Message struct {
	Role       string                 `json:"role"`
	Content    interface{}            `json:"content"`
	Timestamp  time.Time              `json:"timestamp"`
	ToolsUsed  []string               `json:"toolsUsed,omitempty"`
	ToolCalls  interface{}            `json:"tool_calls,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}

// Session represents a conversation session.
type Session struct {
	Key              string                 `json:"key"`
	Messages         []Message              `json:"messages"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	LastConsolidated int                    `json:"last_consolidated"`
}

// New returns a new empty session.
func New(key string) Session {
	now := time.Now()
	return Session{
		Key:       key,
		Messages:  []Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AddMessage appends a message and updates timestamps.
func (s *Session) AddMessage(role string, content interface{}, opts ...MessageOption) {
	msg := Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
	for _, opt := range opts {
		opt(&msg)
	}
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}

// GetHistory returns unconsolidated messages up to maxMessages, aligning to first user.
func (s *Session) GetHistory(maxMessages int) []Message {
	start := s.LastConsolidated
	if start < 0 {
		start = 0
	}
	msgs := s.Messages
	if start > len(msgs) {
		start = len(msgs)
	}
	msgs = msgs[start:]
	if maxMessages > 0 && len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	// drop leading non-user
	for i, m := range msgs {
		if strings.ToLower(m.Role) == "user" {
			msgs = msgs[i:]
			break
		}
	}
	copied := make([]Message, len(msgs))
	copy(copied, msgs)
	return copied
}

// MessageOption mutates a message before append.
type MessageOption func(*Message)

// WithToolsUsed sets tools used.
func WithToolsUsed(tools []string) MessageOption {
	return func(m *Message) { m.ToolsUsed = tools }
}

// WithMeta sets metadata.
func WithMeta(meta map[string]interface{}) MessageOption {
	return func(m *Message) { m.Meta = meta }
}

// WithToolCalls sets assistant tool_calls payload.
func WithToolCalls(calls interface{}) MessageOption {
	return func(m *Message) { m.ToolCalls = calls }
}

// WithToolCallID sets tool_call_id for tool messages.
func WithToolCallID(id string) MessageOption {
	return func(m *Message) { m.ToolCallID = id }
}

// WithName sets optional name field.
func WithName(name string) MessageOption {
	return func(m *Message) { m.Name = name }
}

// Manager handles session storage and caching.
type Manager struct {
	dir       string
	cache     map[string]Session
	mu        sync.RWMutex
	legacyDir string
}

// NewManager creates a manager rooted at dir.
func NewManager(dir string) *Manager {
	return &Manager{
		dir:       dir,
		cache:     map[string]Session{},
		legacyDir: filepath.Join(os.Getenv("HOME"), ".lobster", "sessions"),
	}
}

// path returns filepath for session key.
func (m *Manager) path(key string) string {
	safe := safeFilename(strings.ReplaceAll(key, ":", "_"))
	return filepath.Join(m.dir, safe+".jsonl")
}

func (m *Manager) legacyPath(key string) string {
	safe := safeFilename(strings.ReplaceAll(key, ":", "_"))
	return filepath.Join(m.legacyDir, safe+".jsonl")
}

// GetOrCreate returns a cached or loaded session, creating if missing.
func (m *Manager) GetOrCreate(key string) (Session, error) {
	m.mu.RLock()
	if s, ok := m.cache[key]; ok {
		m.mu.RUnlock()
		return s, nil
	}
	m.mu.RUnlock()

	s, err := m.load(key)
	if errors.Is(err, os.ErrNotExist) {
		s = New(key)
		err = nil
	}
	if err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	m.cache[key] = s
	m.mu.Unlock()
	return s, nil
}

// Save persists a session and caches it.
func (m *Manager) Save(s Session) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir sessions: %w", err)
	}
	path := m.path(s.Key)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	metaLine := map[string]interface{}{
		"_type":             "metadata",
		"key":               s.Key,
		"created_at":        s.CreatedAt,
		"updated_at":        s.UpdatedAt,
		"metadata":          s.Metadata,
		"last_consolidated": s.LastConsolidated,
	}
	if err := writeJSONL(f, metaLine); err != nil {
		return err
	}
	for _, msg := range s.Messages {
		if err := writeJSONL(f, msg); err != nil {
			return err
		}
	}

	m.mu.Lock()
	m.cache[s.Key] = s
	m.mu.Unlock()
	return nil
}

// List returns basic info of all sessions in dir.
func (m *Manager) List() ([]Session, error) {
	files, err := filepath.Glob(filepath.Join(m.dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(files))
	for _, fp := range files {
		s, err := m.loadFromPath(fp)
		if err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// Invalidate removes a session from cache.
func (m *Manager) Invalidate(key string) {
	m.mu.Lock()
	delete(m.cache, key)
	m.mu.Unlock()
}

func (m *Manager) load(key string) (Session, error) {
	path := m.path(key)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// try migrate legacy
		legacy := m.legacyPath(key)
		if _, err2 := os.Stat(legacy); err2 == nil {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				_ = os.Rename(legacy, path)
			}
		}
	}
	return m.loadFromPath(path)
}

func (m *Manager) loadFromPath(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	var (
		messages []Message
		metadata map[string]interface{}
		created  time.Time
		lastCons int
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if t, ok := obj["_type"]; ok && t == "metadata" {
			metadata = toMap(obj["metadata"])
			if ts, ok := obj["created_at"].(string); ok {
				created, _ = time.Parse(time.RFC3339, ts)
			}
			if lc, ok := obj["last_consolidated"].(float64); ok {
				lastCons = int(lc)
			}
			continue
		}
		var msg Message
		if err := mapToStruct(obj, &msg); err == nil {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return Session{}, err
	}
	s := Session{
		Key:              strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Messages:         messages,
		Metadata:         metadata,
		LastConsolidated: lastCons,
	}
	if !created.IsZero() {
		s.CreatedAt = created
	} else {
		s.CreatedAt = time.Now()
	}
	if len(messages) > 0 {
		s.UpdatedAt = messages[len(messages)-1].Timestamp
	} else {
		s.UpdatedAt = s.CreatedAt
	}
	return s, nil
}

func writeJSONL(f *os.File, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal jsonl: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write jsonl: %w", err)
	}
	return nil
}

// safeFilename converts arbitrary string to a safe filename.
func safeFilename(s string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(s)
}

// toMap converts interface{} to map[string]interface{} if possible.
func toMap(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if ok {
		return m
	}
	return nil
}

// mapToStruct decodes a generic map into struct via JSON roundtrip.
func mapToStruct(m map[string]interface{}, out interface{}) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
