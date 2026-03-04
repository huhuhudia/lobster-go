package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath joins workspace and user path, cleaning and optionally enforcing restriction.
// When restrict is true, returns error if resulting path escapes workspace.
func ResolvePath(workspace, userPath string, restrict bool) (string, error) {
	if userPath == "" {
		return "", errors.New("path is required")
	}
	if workspace == "" {
		workspace = "."
	}
	if strings.HasPrefix(userPath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			userPath = filepath.Join(home, strings.TrimPrefix(userPath, "~"))
		}
	}
	var target string
	if filepath.IsAbs(userPath) {
		target = filepath.Clean(userPath)
	} else {
		target = filepath.Join(workspace, userPath)
		target = filepath.Clean(target)
	}
	if !restrict {
		return target, nil
	}
	workspaceClean := filepath.Clean(workspace)
	if !strings.HasPrefix(target, workspaceClean) {
		return "", errors.New("path outside workspace")
	}
	return target, nil
}
