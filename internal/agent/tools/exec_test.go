package tools

import (
	"context"
	"strings"
	"testing"
)

func TestExecRunsCommand(t *testing.T) {
	tool := ExecTool{Workspace: ".", TimeoutSec: 2}
	out, err := tool.Execute(context.Background(), map[string]interface{}{"cmd": "echo hi"})
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecTimeout(t *testing.T) {
	tool := ExecTool{Workspace: ".", TimeoutSec: 1}
	ctx := context.Background()
	_, err := tool.Execute(ctx, map[string]interface{}{"cmd": "sleep 2"})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}
