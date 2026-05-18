package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/tools"
)

// TestResponsesAPI_ToolChoiceAutoExplicit verifies that when tools are
// provided to the Responses API, the request body explicitly contains
// tool_choice=auto. This mirrors the Chat Completions test and ensures
// both API paths have consistent behavior for strict gateways like LiteLLM.
//
// See https://github.com/docker/docker-agent/issues/2804.
func TestResponsesAPI_ToolChoiceAutoExplicit(t *testing.T) {
	t.Parallel()

	var (
		receivedBody []byte
		mu           sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBody = body
		mu.Unlock()
		writeResponsesSSEResponse(w)
	}))
	defer server.Close()

	cfg := &latest.ModelConfig{
		Provider: "custom",
		Model:    "test",
		BaseURL:  server.URL,
		TokenKey: "MY_TOKEN",
		ProviderOpts: map[string]any{
			"api_type": "openai_responses",
		},
	}

	env := environment.NewMapEnvProvider(map[string]string{
		"MY_TOKEN": "secret",
	})

	client, err := NewClient(t.Context(), cfg, env)
	require.NoError(t, err)

	requestTools := []tools.Tool{
		{
			Name:        "search",
			Description: "Search the web",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q": map[string]any{"type": "string"},
				},
			},
		},
	}

	stream, err := client.CreateResponseStream(
		t.Context(),
		[]chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}},
		requestTools,
	)
	require.NoError(t, err)
	defer stream.Close()

	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	mu.Lock()
	defer mu.Unlock()

	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(receivedBody, &payload))

	raw, ok := payload["tool_choice"]
	require.True(t, ok, "tool_choice must be present in the Responses API request body when tools are provided")

	var s string
	require.NoError(t, json.Unmarshal(raw, &s))
	assert.Equal(t, "auto", s)
}

// TestResponsesAPI_NoToolChoiceWithoutTools verifies that when no tools
// are provided to the Responses API, we don't send a tool_choice field.
func TestResponsesAPI_NoToolChoiceWithoutTools(t *testing.T) {
	t.Parallel()

	var (
		receivedBody []byte
		mu           sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBody = body
		mu.Unlock()
		writeResponsesSSEResponse(w)
	}))
	defer server.Close()

	cfg := &latest.ModelConfig{
		Provider: "custom",
		Model:    "test",
		BaseURL:  server.URL,
		TokenKey: "MY_TOKEN",
		ProviderOpts: map[string]any{
			"api_type": "openai_responses",
		},
	}

	env := environment.NewMapEnvProvider(map[string]string{
		"MY_TOKEN": "secret",
	})

	client, err := NewClient(t.Context(), cfg, env)
	require.NoError(t, err)

	stream, err := client.CreateResponseStream(
		t.Context(),
		[]chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}},
		nil,
	)
	require.NoError(t, err)
	defer stream.Close()

	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	mu.Lock()
	defer mu.Unlock()

	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(receivedBody, &payload))

	_, ok := payload["tool_choice"]
	assert.False(t, ok, "tool_choice must not be present in Responses API when no tools are provided")
}

// writeResponsesSSEResponse writes a minimal valid SSE response for the Responses API
func writeResponsesSSEResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`data: {"type":"response.output_item.done","output_item":{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}}`))
	_, _ = w.Write([]byte("\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}
