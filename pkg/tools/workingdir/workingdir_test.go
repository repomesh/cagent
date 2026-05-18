package workingdir

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name              string
		toolsetWorkingDir string
		agentWorkingDir   string
		want              string
	}{
		{name: "empty uses agent working dir", agentWorkingDir: "/workspace", want: "/workspace"},
		{name: "absolute wins", toolsetWorkingDir: "/tmp/app", agentWorkingDir: "/workspace", want: "/tmp/app"},
		{name: "relative joins agent dir", toolsetWorkingDir: "tools/mcp", agentWorkingDir: "/workspace", want: "/workspace/tools/mcp"},
		{name: "relative without agent dir remains relative", toolsetWorkingDir: "tools/mcp", want: "tools/mcp"},
		{name: "tilde expands", toolsetWorkingDir: "~/projects/app", agentWorkingDir: "/workspace", want: filepath.Join(home, "projects", "app")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.toolsetWorkingDir, tt.agentWorkingDir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveEnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_WORKING_DIR_VAR", "/custom/path")

	got := Resolve("${TEST_WORKING_DIR_VAR}/app", "/workspace")
	assert.Equal(t, "/custom/path/app", got)
}
