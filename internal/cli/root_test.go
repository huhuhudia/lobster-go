package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huhuhudia/lobster-go/internal/config"
)

func withStdin(t *testing.T, input string) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
}

func TestRunVersion(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"version"}, &out, &err)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, err.String())
	}
	if got := strings.TrimSpace(out.String()); got == "" {
		t.Fatalf("expected version output, got empty string")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"unknown"}, &out, &err)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(err.String(), "unknown command") {
		t.Fatalf("expected unknown command message, got %q", err.String())
	}
}

func TestRunAgentStub(t *testing.T) {
	var out, err bytes.Buffer
	t.Setenv("LOBSTER_AGENT_EXIT_AFTER_MS", "100ms")
	code := Run([]string{"agent"}, &out, &err)
	if code != 0 {
		t.Fatalf("agent exit code %d, stderr: %s", code, err.String())
	}
	if !strings.Contains(out.String(), "agent running") {
		t.Fatalf("agent output missing, got %q", out.String())
	}
}

func TestRunAgentPrintsConfigLog(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("LOBSTER_AGENT_EXIT_AFTER_MS", "80ms")

	cfg := config.DefaultConfig()
	cfg.Providers["openai"] = config.ProviderConfig{
		APIKey:  "sk-test-abcdef123456",
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
		Model:   "qwen-plus",
	}
	cfg.Agents.Defaults.Provider = "openai"
	cfg.Agents.Defaults.Model = "qwen-plus"
	if err := config.Save(cfg, ""); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var out, err bytes.Buffer
	code := Run([]string{"agent"}, &out, &err)
	if code != 0 {
		t.Fatalf("agent exit code %d, stderr: %s", code, err.String())
	}

	got := out.String()
	if !strings.Contains(got, "config: provider=openai model=qwen-plus") {
		t.Fatalf("missing provider/model log, got: %q", got)
	}
	if !strings.Contains(got, "config: base_url=https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions") {
		t.Fatalf("missing base_url log, got: %q", got)
	}
	if !strings.Contains(got, "config: api_key=sk-t...3456") {
		t.Fatalf("missing masked api key log, got: %q", got)
	}
}

func TestSessionListNoSessions(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"session", "list"}, &out, &err)
	if code != 0 {
		t.Fatalf("session list exit %d, stderr: %s", code, err.String())
	}
	if !strings.Contains(out.String(), "(no sessions)") {
		t.Fatalf("expected no sessions message, got %q", out.String())
	}
}

func TestRunOnboardNewConfig(t *testing.T) {
	// Use temp home directory
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	var out, err bytes.Buffer
	code := Run([]string{"onboard"}, &out, &err)
	if code != 0 {
		t.Fatalf("onboard exit code %d, stderr: %s", code, err.String())
	}
	if !strings.Contains(out.String(), "Created config") {
		t.Fatalf("expected config creation message, got %q", out.String())
	}

	// Verify config file exists
	cfgPath := filepath.Join(tmpDir, ".lobster", "config.json")
	if _, e := os.Stat(cfgPath); os.IsNotExist(e) {
		t.Fatalf("config file not created at %s", cfgPath)
	}

	// Verify workspace exists
	workspace := filepath.Join(tmpDir, ".lobster", "workspace")
	if _, e := os.Stat(workspace); os.IsNotExist(e) {
		t.Fatalf("workspace not created at %s", workspace)
	}

	// Verify templates are synced
	expected := []string{
		"AGENTS.md",
		"TOOLS.md",
		"USER.md",
		"SOUL.md",
		"HEARTBEAT.md",
		"memory/MEMORY.md",
		"memory/HISTORY.md",
	}
	for _, rel := range expected {
		path := filepath.Join(workspace, rel)
		if _, e := os.Stat(path); os.IsNotExist(e) {
			t.Fatalf("template not created at %s", path)
		}
	}
}

func TestRunOnboardIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	var out1, err1 bytes.Buffer
	if code := Run([]string{"onboard"}, &out1, &err1); code != 0 {
		t.Fatalf("first onboard exit %d, stderr: %s", code, err1.String())
	}

	withStdin(t, "\n")
	var out2, err2 bytes.Buffer
	if code := Run([]string{"onboard"}, &out2, &err2); code != 0 {
		t.Fatalf("second onboard exit %d, stderr: %s", code, err2.String())
	}
	if !strings.Contains(out2.String(), "Config already exists") {
		t.Fatalf("expected existing-config prompt, got %q", out2.String())
	}

	workspace := filepath.Join(tmpDir, ".lobster", "workspace")
	if _, e := os.Stat(filepath.Join(workspace, "memory", "MEMORY.md")); os.IsNotExist(e) {
		t.Fatalf("expected MEMORY.md to remain after second onboard")
	}
}

func TestRunCronList(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"cron", "list"}, &out, &err)
	if code != 0 {
		t.Fatalf("cron list exit code %d, stderr: %s", code, err.String())
	}
	if !strings.Contains(out.String(), "Scheduled jobs") {
		t.Fatalf("expected scheduled jobs message, got %q", out.String())
	}
}

func TestAgentLoopConfigFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "gpt-x"
	cfg.Agents.Defaults.Temperature = 0.7
	cfg.Agents.Defaults.MaxTokens = 2048
	cfg.Tools.RestrictToWorkspace = true
	cfg.Tools.ExecTimeoutSec = 45
	cfg.Memory.ConsolidateEvery = 6
	cfg.Memory.WindowSize = 12
	cfg.Memory.Mode = "archive_all"

	got := agentLoopConfigFromConfig(cfg, "/tmp/ws")
	if got.Model != "gpt-x" {
		t.Fatalf("model mismatch: %s", got.Model)
	}
	if got.Temperature != 0.7 {
		t.Fatalf("temperature mismatch: %v", got.Temperature)
	}
	if got.MaxTokens != 2048 {
		t.Fatalf("max tokens mismatch: %d", got.MaxTokens)
	}
	if !got.RestrictToWorkspace {
		t.Fatalf("restrict flag not propagated")
	}
	if got.ExecTimeoutSec != 45 {
		t.Fatalf("exec timeout mismatch: %d", got.ExecTimeoutSec)
	}
	if got.MemoryConsolidateEvery != 6 {
		t.Fatalf("memory consolidate every mismatch: %d", got.MemoryConsolidateEvery)
	}
	if got.MemoryWindowSize != 12 {
		t.Fatalf("memory window size mismatch: %d", got.MemoryWindowSize)
	}
	if got.MemoryMode != "archive_all" {
		t.Fatalf("memory mode mismatch: %s", got.MemoryMode)
	}
}

func TestDurationFromSec(t *testing.T) {
	if got := durationFromSec(0, 30); got != 30*time.Second {
		t.Fatalf("expected fallback duration, got %v", got)
	}
	if got := durationFromSec(12, 30); got != 12*time.Second {
		t.Fatalf("expected explicit duration, got %v", got)
	}
}

func TestHelpCommand(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"help"}, &out, &err)
	if code != 0 {
		t.Fatalf("help exit code %d, stderr: %s", code, err.String())
	}
	expectedCommands := []string{"onboard", "agent", "session", "cron", "heartbeat", "version"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(out.String(), cmd) {
			t.Fatalf("help output missing command %q, got %q", cmd, out.String())
		}
	}
}
