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

// TestChatCompletions_ToolChoiceAutoExplicit verifies that when tools are
// provided, the request body explicitly contains tool_choice=auto. This is
// required by some strict OpenAI-compatible gateways (e.g. LiteLLM) that
// reject requests with a missing tool_choice field even though the OpenAI
// spec treats omission as equivalent to "auto".
//
// See https://github.com/docker/docker-agent/issues/2804.
func TestChatCompletions_ToolChoiceAutoExplicit(t *testing.T) {
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
		writeSSEResponse(w)
	}))
	defer server.Close()

	cfg := &latest.ModelConfig{
		Provider: "custom",
		Model:    "test",
		BaseURL:  server.URL,
		TokenKey: "MY_TOKEN",
		ProviderOpts: map[string]any{
			"api_type": "openai_chatcompletions",
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

	stream, err := client.CreateChatCompletionStream(
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
	require.True(t, ok, "tool_choice must be present in the request body when tools are provided")

	var s string
	require.NoError(t, json.Unmarshal(raw, &s))
	assert.Equal(t, "auto", s)
}

// TestChatCompletions_NoToolChoiceWithoutTools verifies that when no tools
// are provided we don't send a tool_choice field (which would be invalid).
func TestChatCompletions_NoToolChoiceWithoutTools(t *testing.T) {
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
		writeSSEResponse(w)
	}))
	defer server.Close()

	cfg := &latest.ModelConfig{
		Provider: "custom",
		Model:    "test",
		BaseURL:  server.URL,
		TokenKey: "MY_TOKEN",
		ProviderOpts: map[string]any{
			"api_type": "openai_chatcompletions",
		},
	}

	env := environment.NewMapEnvProvider(map[string]string{
		"MY_TOKEN": "secret",
	})

	client, err := NewClient(t.Context(), cfg, env)
	require.NoError(t, err)

	stream, err := client.CreateChatCompletionStream(
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
	assert.False(t, ok, "tool_choice must not be present when no tools are provided")
}
