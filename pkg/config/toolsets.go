package config

import (
	"fmt"

	"github.com/docker/docker-agent/pkg/config/latest"
)

// resolveToolsetDefinitions appends reusable toolset definitions referenced by
// agents (via use_toolsets) from the top-level toolsets section onto each
// agent's Toolsets slice. Inline toolsets defined on the agent come first,
// followed by the toolsets from each referenced definition in order. This
// mirrors the use_commands / use_skills group convention but works for any
// toolset type.
//
// It runs before resolveMCPDefinitions / resolveRAGDefinitions so that mcp and
// rag toolsets pulled in from a definition still have their refs resolved.
func resolveToolsetDefinitions(cfg *latest.Config) error {
	for i := range cfg.Agents {
		agent := &cfg.Agents[i]
		for _, ref := range agent.UseToolsets {
			ts, ok := cfg.Toolsets[ref]
			if !ok {
				return fmt.Errorf("agent '%s' references non-existent toolset '%s'", agent.Name, ref)
			}
			agent.Toolsets = append(agent.Toolsets, ts)
		}
	}
	return nil
}
