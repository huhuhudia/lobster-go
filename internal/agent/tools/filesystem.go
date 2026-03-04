package tools

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/huhuhudia/lobster-go/internal/providers"
	"github.com/huhuhudia/lobster-go/pkg/utils"
)

// ListDirTool lists files in a directory.
type ListDirTool struct {
	Workspace string
	Restrict  bool
}

func (t ListDirTool) Name() string { return "list_dir" }

func (t ListDirTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "List files and directories under a path",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative or absolute directory path",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t ListDirTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, err := extractPath(args)
	if err != nil {
		return "", err
	}
	full, err := utils.ResolvePath(t.Workspace, path, t.Restrict)
	if err != nil {
		return "", err
	}
	entries, err := ioutil.ReadDir(full)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

// ReadFileTool reads a file.
type ReadFileTool struct {
	Workspace string
	Restrict  bool
	MaxBytes  int
}

func (t ReadFileTool) Name() string { return "read_file" }

func (t ReadFileTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "Read a text file",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative or absolute file path",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, err := extractPath(args)
	if err != nil {
		return "", err
	}
	full, err := utils.ResolvePath(t.Workspace, path, t.Restrict)
	if err != nil {
		return "", err
	}
	data, err := ioutil.ReadFile(full)
	if err != nil {
		return "", err
	}
	if t.MaxBytes > 0 && len(data) > t.MaxBytes {
		data = data[:t.MaxBytes]
	}
	return string(data), nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct {
	Workspace string
	Restrict  bool
}

func (t WriteFileTool) Name() string { return "write_file" }

func (t WriteFileTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: map[string]interface{}{
			"name":        t.Name(),
			"description": "Write text to a file (overwrites)",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative or absolute file path",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Text content to write",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (t WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, err := extractPath(args)
	if err != nil {
		return "", err
	}
	content, ok := args["content"].(string)
	if !ok {
		return "", errors.New("content must be string")
	}
	full, err := utils.ResolvePath(t.Workspace, path, t.Restrict)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "ok", nil
}

func extractPath(args map[string]interface{}) (string, error) {
	pathVal, ok := args["path"]
	if !ok {
		return "", errors.New("path required")
	}
	path, ok := pathVal.(string)
	if !ok {
		return "", errors.New("path must be string")
	}
	return path, nil
}
