---
title: "Plan Tool"
description: "Shared persistent scratchpad for multi-agent collaboration."
permalink: /tools/plan/
---

# Plan Tool

_Shared persistent scratchpad for multi-agent collaboration._

## Overview

The plan tool gives agents a shared, persistent scratchpad of named documents. Any agent in a multi-agent config that loads the `plan` toolset can read and write the same plans, and those plans survive across sessions. This makes it straightforward to wire a planner agent that sketches work and one or more executor agents that consume it without any custom tool wiring.

Plans are stored as JSON files in the docker-agent data directory (`~/.cagent/plans/` by default). All agents that share a process serialize on a single mutex so concurrent reads and writes are safe. Writes are atomic (temp file + rename), so a reader never observes partial content.

## Configuration

```yaml
toolsets:
  - type: plan
```

No additional options are required. All agents that include `type: plan` in their toolsets share the same plans.

## Available Tools

| Tool          | Description                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------- |
| `write_plan`  | Create or update a shared plan by name. Replaces the entire plan content — read it first to preserve what you want to keep. Each write bumps the revision number. |
| `read_plan`   | Read a shared plan by name, including its title, content, author, revision number, and last-updated timestamp. |
| `list_plans`  | List all shared plans with their name, title, author, revision, and last-updated timestamp. |
| `delete_plan` | Delete a shared plan by name.                                                                     |

### Plan Names

Plan names must match the pattern `[a-z0-9][a-z0-9_-]*` (lowercase letters, digits, `-`, `_`). This is enforced structurally so two different inputs can never collapse onto the same file and path-traversal is impossible by construction.

### Plan Fields

Each plan document contains:

| Field      | Description                                               |
| ---------- | --------------------------------------------------------- |
| `name`     | The plan's unique slug name                               |
| `title`    | A short human-readable title (optional)                   |
| `content`  | The full Markdown or free-form plan text                  |
| `author`   | Free-form label identifying who last wrote the plan       |
| `revision` | Monotonically increasing counter, bumped on every write   |
| `updatedAt`| ISO 8601 timestamp of the last write                      |

## Example

Two agents collaborate on a shared plan — the architect drafts it and the builder refines it:

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-5
    description: Coordinator
    instruction: |
      Route work between the architect and the builder.
    handoffs: [architect, builder]

  architect:
    model: anthropic/claude-sonnet-4-5
    description: Drafts high-level plans
    instruction: |
      Use list_plans and read_plan to inspect existing plans, then write_plan
      to create or revise one. Always read before writing. When done, hand off
      to the builder.
    toolsets:
      - type: plan
    handoffs: [builder]

  builder:
    model: openai/gpt-4o
    description: Adds implementation steps to plans
    instruction: |
      Read the architect's plan with read_plan, then use write_plan to append
      concrete implementation steps. Always read before writing. When done,
      hand off back to root.
    toolsets:
      - type: plan
    handoffs: [root]
```

See [`examples/shared_plan.yaml`](https://github.com/docker/docker-agent/blob/main/examples/shared_plan.yaml) for a complete working example.

## Error Handling

- `read_plan` returns a distinct "not found" error when a plan does not exist, as opposed to any other I/O error, so callers can tell "plan missing" from "plan unreadable."
- `list_plans` skips corrupt entries but reports them in a `warnings` field so an agent can detect and recover from a bad state (e.g., by calling `delete_plan`).
- `delete_plan` can remove a corrupt plan to recover from a bad state.

<div class="callout callout-tip" markdown="1">
<div class="callout-title">Plan vs. Todo vs. Tasks
</div>
  <p>Use <strong>plan</strong> for shared, free-form documents that multiple agents collaborate on (design docs, requirements, work items). Use <a href="{{ '/tools/todo/' | relative_url }}">todo</a> for lightweight in-session task lists. Use <a href="{{ '/tools/tasks/' | relative_url }}">tasks</a> for a structured, persistent task database with priorities and dependencies.</p>
</div>
