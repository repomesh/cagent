package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/filesystem"
	shelltool "github.com/docker/docker-agent/pkg/tools/builtin/shell"
	"github.com/docker/docker-agent/pkg/tui/core/layout"
	"github.com/docker/docker-agent/pkg/tui/service"
	"github.com/docker/docker-agent/pkg/tui/types"
)

// withCleanToolRegistry snapshots the package-global custom renderer registry and
// restores it when the test finishes, so Register calls don't leak across tests.
func withCleanToolRegistry(t *testing.T) {
	t.Helper()
	customMu.Lock()
	saved := custom
	custom = map[string]Builder{}
	customMu.Unlock()
	t.Cleanup(func() {
		customMu.Lock()
		custom = saved
		customMu.Unlock()
	})
}

func TestRegisterAndResolve(t *testing.T) {
	withCleanToolRegistry(t)

	// Unknown, unregistered key resolves to nothing.
	_, ok := resolve("add")
	assert.False(t, ok)

	// A new tool name resolves to its registered renderer.
	customCalled := false
	Register("add", func(*types.Message, service.SessionStateReader) layout.Model {
		customCalled = true
		return nil
	})
	b, ok := resolve("add")
	assert.True(t, ok)
	b(nil, nil)
	assert.True(t, customCalled)

	// A custom renderer takes precedence over a built-in one for the same key.
	overrodeBuiltin := false
	Register(shelltool.ToolNameShell, func(*types.Message, service.SessionStateReader) layout.Model {
		overrodeBuiltin = true
		return nil
	})
	b, ok = resolve(shelltool.ToolNameShell)
	assert.True(t, ok)
	b(nil, nil)
	assert.True(t, overrodeBuiltin)

	// A built-in with no custom override still resolves to its built-in renderer.
	_, ok = resolve(filesystem.ToolNameReadFile)
	assert.True(t, ok)
}

// TestNew_Dispatch verifies New()'s renderer selection: a registered renderer is
// chosen by exact tool name first, then by "category:<category>", with the exact
// name winning when both match, and an unregistered tool falling through to the
// default. The factory is origin-agnostic — it keys only on the tool-call name
// and category — so this holds for built-in, Go-SDK, and MCP tools alike. (For an
// end-to-end custom renderer over a real MCP tool, see examples/golibrary/renderer.)
func TestNew_Dispatch(t *testing.T) {
	ss := service.StaticSessionState{}

	newMsg := func() *types.Message {
		return &types.Message{
			ToolCall:       tools.ToolCall{Function: tools.FunctionCall{Name: "weather_report"}},
			ToolDefinition: tools.Tool{Name: "weather_report", Category: "external"},
		}
	}

	t.Run("by exact tool name", func(t *testing.T) {
		withCleanToolRegistry(t)
		called := false
		Register("weather_report", func(*types.Message, service.SessionStateReader) layout.Model {
			called = true
			return nil
		})
		New(newMsg(), ss)
		assert.True(t, called, "renderer registered under the exact tool name should be selected")
	})

	t.Run("by category", func(t *testing.T) {
		withCleanToolRegistry(t)
		called := false
		Register("category:external", func(*types.Message, service.SessionStateReader) layout.Model {
			called = true
			return nil
		})
		New(newMsg(), ss)
		assert.True(t, called, "a category renderer should match any tool in that category")
	})

	t.Run("exact name wins over category", func(t *testing.T) {
		withCleanToolRegistry(t)
		exactCalled, categoryCalled := false, false
		Register("weather_report", func(*types.Message, service.SessionStateReader) layout.Model {
			exactCalled = true
			return nil
		})
		Register("category:external", func(*types.Message, service.SessionStateReader) layout.Model {
			categoryCalled = true
			return nil
		})
		New(newMsg(), ss)
		assert.True(t, exactCalled, "exact-name renderer should take precedence")
		assert.False(t, categoryCalled, "category renderer should not run when an exact-name match exists")
	})

	t.Run("unregistered tool falls through to default", func(t *testing.T) {
		withCleanToolRegistry(t)
		_, byName := resolve("weather_report")
		_, byCategory := resolve("category:external")
		assert.False(t, byName, "no per-tool renderer registered")
		assert.False(t, byCategory, "no category renderer registered")
	})
}
