package toolsetpath

import (
	"path/filepath"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/path"
)

// Resolve returns the validated absolute path for a toolset-specific file or
// directory (e.g. memory database, tasks file). It expands ~ and ${VAR},
// resolves relative paths against runConfig.WorkingDir or parentDir, and
// validates that the result is contained within the base directory.
//
// Resolution rules:
//   - Shell patterns (~ and ${VAR}/$VAR) are expanded first.
//   - If the expanded path is absolute, basePath is empty (no containment check).
//   - If the expanded path is relative and runConfig.WorkingDir is non-empty,
//     basePath is runConfig.WorkingDir.
//   - If the expanded path is relative and runConfig.WorkingDir is empty,
//     basePath is parentDir.
//
// The final path is validated via path.ValidatePathInDirectory to prevent
// directory traversal attacks.
func Resolve(toolsetPath, parentDir string, runConfig *config.RuntimeConfig) (string, error) {
	toolsetPath = path.ExpandPath(toolsetPath)

	var basePath string
	if filepath.IsAbs(toolsetPath) {
		basePath = ""
	} else if wd := runConfig.WorkingDir; wd != "" {
		basePath = wd
	} else {
		basePath = parentDir
	}

	return path.ValidatePathInDirectory(toolsetPath, basePath)
}
