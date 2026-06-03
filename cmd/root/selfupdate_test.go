package root

import (
	"testing"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestIsManagementInvocation(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{metadata.MetadataSubcommandName},
		{cobra.ShellCompRequestCmd},
		{cobra.ShellCompNoDescRequestCmd},
		{"completion", "bash"},
		{"version"},
		{"help"},
		{"--help"},
		{"-h"},
		{"--version"},
	} {
		assert.True(t, isManagementInvocation(args), "args %v", args)
	}

	for _, args := range [][]string{
		nil,
		{},
		{"run", "agent.yaml"},
		{"new"},
	} {
		assert.False(t, isManagementInvocation(args), "args %v", args)
	}
}
