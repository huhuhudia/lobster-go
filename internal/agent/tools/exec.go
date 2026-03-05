package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/huhuhudia/lobster-go/internal/providers"
)

// ExecTool executes shell commands within workspace.
type ExecTool struct {
	Workspace  string
	Restrict   bool
	TimeoutSec int
	PathAppend []string
}

func (t ExecTool) Name() string { return "exec" }

func (t ExecTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "Run a shell command",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cmd": map[string]interface{}{
						"type":        "string",
						"description": "Command to run",
					},
				},
				"required": []string{"cmd"},
			},
		},
	}
}

func (t ExecTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	cmdStr, ok := args["cmd"].(string)
	if !ok || strings.TrimSpace(cmdStr) == "" {
		return "", errors.New("cmd must be non-empty string")
	}
	if err := validateExecCommand(cmdStr); err != nil {
		return "", err
	}
	timeout := time.Duration(t.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
	if t.Workspace != "" {
		cmd.Dir = t.Workspace
	}
	// PATH append
	if len(t.PathAppend) > 0 {
		cmd.Env = append(cmd.Env, "PATH="+strings.Join(t.PathAppend, ":")+":"+getEnv("PATH"))
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", ctx.Err()
	}
	if err != nil {
		errText := strings.TrimSpace(errBuf.String())
		if errText == "" {
			errText = err.Error()
		}
		return "", errors.New(truncateOutput(errText))
	}
	return truncateOutput(strings.TrimSpace(outBuf.String())), nil
}

func getEnv(k string) string {
	if v, ok := lookupEnv(k); ok {
		return v
	}
	return ""
}

var lookupEnv = os.LookupEnv

const execOutputMaxChars = 10_000

var blockedCommandFragments = []string{
	"rm -rf /",
	"rm -rf ~",
	"mkfs",
	"shutdown",
	"reboot",
	"poweroff",
	"halt",
	":(){:|:&};:",
}

func validateExecCommand(cmd string) error {
	normalized := strings.ToLower(strings.TrimSpace(cmd))
	for _, fragment := range blockedCommandFragments {
		if strings.Contains(normalized, fragment) {
			return fmt.Errorf("command blocked by safety policy: %s", fragment)
		}
	}
	return nil
}

func truncateOutput(s string) string {
	if len(s) <= execOutputMaxChars {
		return s
	}
	return s[:execOutputMaxChars]
}
