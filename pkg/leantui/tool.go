package leantui

import (
	"fmt"
	"strings"

	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tui/animation"
	toolcomponent "github.com/docker/docker-agent/pkg/tui/components/tool"
	"github.com/docker/docker-agent/pkg/tui/service"
	tuitypes "github.com/docker/docker-agent/pkg/tui/types"
)

// toolView is the render state of a single tool call. It deliberately stores
// the same TUI message shape used by the full-screen TUI so the lean renderer
// can delegate the visual representation to pkg/tui/components/tool.
type toolView struct {
	message *tuitypes.Message
	images  []inlineImage
}

const maxToolOutputLines = 12

func newToolView(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status tuitypes.ToolStatus) *toolView {
	return &toolView{
		message: tuitypes.ToolCallMessage(agentName, toolCall, ensureToolDefinition(toolCall, toolDef), status),
	}
}

func ensureToolDefinition(toolCall tools.ToolCall, toolDef tools.Tool) tools.Tool {
	if toolDef.Name == "" {
		toolDef.Name = toolCall.Function.Name
	}
	return toolDef
}

// renderTool renders a tool call with the same renderer registry used by the
// full TUI. This keeps built-in tools and registered custom renderers visually
// consistent between the normal and lean interfaces.
func renderTool(t toolView, width int) []string {
	return renderToolWithState(t, width, 0, service.StaticSessionState{})
}

func renderToolWithState(t toolView, width, frame int, sessionState service.SessionStateReader) []string {
	if width < 1 {
		width = 1
	}
	if t.message == nil {
		return nil
	}
	if sessionState == nil {
		sessionState = service.StaticSessionState{}
	}

	view := toolcomponent.New(t.message, sessionState)
	view.SetSize(width, 0)
	if t.message.ToolStatus == tuitypes.ToolStatusPending || t.message.ToolStatus == tuitypes.ToolStatusRunning {
		view, _ = view.Update(animation.TickMsg{Frame: frame})
		defer animation.StopView(view)
	}

	lines := splitRenderedTool(view.View(), width)
	for _, img := range t.images {
		lines = append(lines, renderInlineImage(img, width)...)
	}
	return lines
}

func splitRenderedTool(rendered string, width int) []string {
	if width < 1 {
		width = 1
	}
	rendered = strings.TrimRight(rendered, "\n")
	if rendered == "" {
		return nil
	}

	var out []string
	for line := range strings.SplitSeq(rendered, "\n") {
		if displayWidth(line) > width {
			out = append(out, wrapANSI(line, width)...)
			continue
		}
		out = append(out, line)
	}
	return out
}

func renderToolOutput(output string, width int) []string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	var out []string
	if len(lines) > maxToolOutputLines {
		hidden := len(lines) - maxToolOutputLines
		out = append(out, "  "+stMuted().Render(fmt.Sprintf("… (%d earlier lines)", hidden)))
		lines = lines[len(lines)-maxToolOutputLines:]
	}
	for _, l := range lines {
		for _, wl := range wrapANSI(l, width-2) {
			out = append(out, "  "+stMuted().Render(wl))
		}
	}
	return out
}
