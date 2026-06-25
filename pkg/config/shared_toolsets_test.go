package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedToolsets_Resolve(t *testing.T) {
	t.Parallel()

	cfg, err := Load(t.Context(), NewFileSource("testdata/shared_toolsets.yaml"))
	require.NoError(t, err)

	dev, ok := cfg.Agents.Lookup("dev")
	require.True(t, ok)
	// Inline toolset comes first, followed by the referenced toolsets in order.
	require.Len(t, dev.Toolsets, 3)
	assert.Equal(t, "think", dev.Toolsets[0].Type)
	assert.Equal(t, "filesystem", dev.Toolsets[1].Type)
	assert.Equal(t, "fetch", dev.Toolsets[2].Type)
	assert.Equal(t, []string{"docker.com"}, dev.Toolsets[2].AllowedDomains)

	// The same definitions are reusable across agents.
	ops, ok := cfg.Agents.Lookup("ops")
	require.True(t, ok)
	require.Len(t, ops.Toolsets, 1)
	assert.Equal(t, "filesystem", ops.Toolsets[0].Type)
}

func TestSharedToolsets_MissingDefinition(t *testing.T) {
	t.Parallel()

	_, err := Load(t.Context(), NewFileSource("testdata/shared_toolsets_missing.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent toolset 'nonexistent'")
}

func TestSharedToolsets_InvalidDefinitionValidated(t *testing.T) {
	t.Parallel()

	// Top-level toolset definitions are validated even when no agent references them.
	_, err := Load(t.Context(), NewFileSource("testdata/shared_toolsets_invalid.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allow_list can only be used with type 'filesystem'")
}
