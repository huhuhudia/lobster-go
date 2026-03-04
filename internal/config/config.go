package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProviderConfig defines a single LLM provider entry.
type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"`
	Model   string `json:"model,omitempty"`
}

// AgentDefaults holds default agent settings.
type AgentDefaults struct {
	Model       string  `json:"model,omitempty"`
	Provider    string  `json:"provider,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
}

// AgentsConfig groups default agent settings.
type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

// ToolsConfig covers global tool settings.
type ToolsConfig struct {
	RestrictToWorkspace bool `json:"restrictToWorkspace"`
	ExecTimeoutSec      int  `json:"execTimeoutSec,omitempty"`
}

// Config is the root configuration schema.
type Config struct {
	Providers map[string]ProviderConfig `json:"providers,omitempty"`
	Agents    AgentsConfig              `json:"agents"`
	Tools     ToolsConfig               `json:"tools"`
}

// DefaultConfig returns a config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Providers: map[string]ProviderConfig{},
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Model:       "",
				Provider:    "",
				Temperature: 0.1,
				MaxTokens:   4096,
			},
		},
		Tools: ToolsConfig{
			RestrictToWorkspace: false,
			ExecTimeoutSec:      120,
		},
	}
}

// Path returns the default config path: ~/.lobster/config.json.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".lobster", "config.json"), nil
}

// Load reads config from the given path; when empty, uses Path().
// Missing file returns DefaultConfig.
func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = Path()
		if err != nil {
			return Config{}, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg, err := parseConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// Save writes config to the given path; when empty, uses Path().
func Save(cfg Config, path string) error {
	if path == "" {
		var err error
		path, err = Path()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// parseConfig accepts both camelCase and snake_case keys by normalizing maps before decoding.
func parseConfig(data []byte) (Config, error) {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	normalized := normalizeKeys(raw)
	b, err := json.Marshal(normalized)
	if err != nil {
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// normalizeKeys recursively converts snake_case map keys to camelCase.
func normalizeKeys(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, vv := range val {
			out[toCamel(k)] = normalizeKeys(vv)
		}
		return out
	case []interface{}:
		for i := range val {
			val[i] = normalizeKeys(val[i])
		}
		return val
	default:
		return v
	}
}

// toCamel converts snake_case to camelCase, leaving other forms unchanged.
func toCamel(s string) string {
	if !strings.ContainsRune(s, '_') {
		return s
	}
	parts := strings.Split(s, "_")
	for i := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(parts[i])
			continue
		}
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, "")
}
