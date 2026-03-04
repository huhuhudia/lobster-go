package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
