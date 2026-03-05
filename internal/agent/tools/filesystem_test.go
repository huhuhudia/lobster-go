package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestListDirAndReadWrite(t *testing.T) {
	tmp := t.TempDir()
	write := WriteFileTool{Workspace: tmp, Restrict: true}
	if _, err := write.Execute(context.Background(), map[string]interface{}{"path": "a.txt", "content": "hello"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	list := ListDirTool{Workspace: tmp, Restrict: true}
	out, err := list.Execute(context.Background(), map[string]interface{}{"path": "."})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out == "" || out[:1] != "a" {
		t.Fatalf("unexpected list output: %s", out)
	}

	read := ReadFileTool{Workspace: tmp, Restrict: true}
	content, err := read.Execute(context.Background(), map[string]interface{}{"path": "a.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if content != "hello" {
		t.Fatalf("content mismatch: %s", content)
	}
}

func TestRestrictPreventsEscape(t *testing.T) {
	tmp := t.TempDir()
	read := ReadFileTool{Workspace: tmp, Restrict: true}
	_, err := read.Execute(context.Background(), map[string]interface{}{"path": "/etc/passwd"})
	if err == nil {
		t.Fatalf("expected restriction error")
	}
}

func TestRestrictPreventsListEscape(t *testing.T) {
	tmp := t.TempDir()
	list := ListDirTool{Workspace: tmp, Restrict: true}
	_, err := list.Execute(context.Background(), map[string]interface{}{"path": "/etc"})
	if err == nil {
		t.Fatalf("expected restriction error")
	}
}

func TestRestrictPreventsWriteEscape(t *testing.T) {
	tmp := t.TempDir()
	write := WriteFileTool{Workspace: tmp, Restrict: true}
	_, err := write.Execute(context.Background(), map[string]interface{}{
		"path":    "/tmp/outside.txt",
		"content": "x",
	})
	if err == nil {
		t.Fatalf("expected restriction error")
	}
}

func TestResolveHomePath(t *testing.T) {
	tmp := t.TempDir()
	homeFile := filepath.Join(tmp, "b.txt")
	if err := os.WriteFile(homeFile, []byte("hi"), 0o644); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	t.Setenv("HOME", tmp)
	read := ReadFileTool{Workspace: tmp, Restrict: false}
	content, err := read.Execute(context.Background(), map[string]interface{}{"path": "~/b.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if content != "hi" {
		t.Fatalf("home content mismatch: %s", content)
	}
}
