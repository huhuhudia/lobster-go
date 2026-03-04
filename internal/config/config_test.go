package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathUsesHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	p, err := Path()
	if err != nil {
		t.Fatalf("Path error: %v", err)
	}
	if !strings.HasPrefix(p, filepath.Join(tmp, ".lobster")) {
		t.Fatalf("expected path under HOME/.lobster, got %s", p)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Tools.ExecTimeoutSec != 120 {
		t.Fatalf("default ExecTimeoutSec mismatch, got %d", cfg.Tools.ExecTimeoutSec)
	}
}

func TestLoadSnakeCase(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	// snake_case keys should map to camelCase fields
	data := `{
		"providers": {
			"openai": {"api_key": "sk-test", "base_url": "https://api.example.com"}
		},
		"agents": {
			"defaults": {
				"model": "gpt-test",
				"provider": "openai",
				"temperature": 0.3,
				"max_tokens": 1234
			}
		},
		"tools": {
			"restrict_to_workspace": true,
			"exec_timeout_sec": 99
		}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Providers["openai"].APIKey != "sk-test" {
		t.Fatalf("api key not parsed")
	}
	if cfg.Agents.Defaults.MaxTokens != 1234 {
		t.Fatalf("max tokens mismatch: %d", cfg.Agents.Defaults.MaxTokens)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Fatalf("restrict flag not parsed")
	}
	if cfg.Tools.ExecTimeoutSec != 99 {
		t.Fatalf("exec timeout mismatch: %d", cfg.Tools.ExecTimeoutSec)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	orig := DefaultConfig()
	orig.Providers["openai"] = ProviderConfig{APIKey: "sk-abc", Model: "gpt-4"}
	orig.Agents.Defaults.Model = "gpt-4"
	if err := Save(orig, path); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.Providers["openai"].APIKey != "sk-abc" {
		t.Fatalf("roundtrip api key mismatch")
	}
	if loaded.Agents.Defaults.Model != "gpt-4" {
		t.Fatalf("roundtrip model mismatch")
	}
}
