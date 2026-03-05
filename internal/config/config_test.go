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
	if cfg.Services.CronIntervalSec != 60 {
		t.Fatalf("default CronIntervalSec mismatch, got %d", cfg.Services.CronIntervalSec)
	}
	if cfg.Services.HeartbeatIntervalSec != 30 {
		t.Fatalf("default HeartbeatIntervalSec mismatch, got %d", cfg.Services.HeartbeatIntervalSec)
	}
	if cfg.Memory.ConsolidateEvery != 20 {
		t.Fatalf("default ConsolidateEvery mismatch, got %d", cfg.Memory.ConsolidateEvery)
	}
	if cfg.Memory.WindowSize != 50 {
		t.Fatalf("default WindowSize mismatch, got %d", cfg.Memory.WindowSize)
	}
	if cfg.Memory.Mode != "window" {
		t.Fatalf("default Mode mismatch, got %s", cfg.Memory.Mode)
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
		},
		"services": {
			"cron_interval_sec": 15,
			"heartbeat_interval_sec": 7
		},
		"memory": {
			"consolidate_every": 3,
			"window_size": 8,
			"mode": "archive_all"
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
	if cfg.Services.CronIntervalSec != 15 {
		t.Fatalf("cron interval mismatch: %d", cfg.Services.CronIntervalSec)
	}
	if cfg.Services.HeartbeatIntervalSec != 7 {
		t.Fatalf("heartbeat interval mismatch: %d", cfg.Services.HeartbeatIntervalSec)
	}
	if cfg.Memory.ConsolidateEvery != 3 {
		t.Fatalf("consolidate_every mismatch: %d", cfg.Memory.ConsolidateEvery)
	}
	if cfg.Memory.WindowSize != 8 {
		t.Fatalf("window_size mismatch: %d", cfg.Memory.WindowSize)
	}
	if cfg.Memory.Mode != "archive_all" {
		t.Fatalf("mode mismatch: %s", cfg.Memory.Mode)
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
