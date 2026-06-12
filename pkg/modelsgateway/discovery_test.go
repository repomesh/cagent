package modelsgateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/environment"
)

// Note: httptest servers listen on 127.0.0.1, which IsTrustedDockerURL
// treats as trusted, so every test against them exercises the Docker
// token auth path.

func tokenEnv() environment.Provider {
	return environment.NewMapEnvProvider(map[string]string{
		environment.DockerDesktopTokenEnv: "test-token",
	})
}

func TestListModels(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"openai/gpt-4o"},{"id":"anthropic/claude-sonnet-4-0"},{"id":""}]}`))
	}))
	defer server.Close()

	ids, err := ListModels(t.Context(), server.URL, tokenEnv())

	require.NoError(t, err)
	assert.Equal(t, []string{"openai/gpt-4o", "anthropic/claude-sonnet-4-0"}, ids)
	assert.Equal(t, "/v1/models", gotPath)
	assert.Equal(t, "Bearer test-token", gotAuth)
}

func TestListModels_GatewayPathAndQuery(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer server.Close()

	ids, err := ListModels(t.Context(), server.URL+"/gateway/?api-version=2", tokenEnv())

	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-4o"}, ids)
	assert.Equal(t, "/gateway/v1/models", gotPath)
	assert.Equal(t, "api-version=2", gotQuery)
}

func TestListModels_EmptyList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()

	ids, err := ListModels(t.Context(), server.URL, tokenEnv())

	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestListModels_Unsupported(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := ListModels(t.Context(), server.URL, tokenEnv())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestListModels_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))
	defer server.Close()

	_, err := ListModels(t.Context(), server.URL, tokenEnv())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding gateway models response")
}

func TestListModels_MissingDockerToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer server.Close()

	_, err := ListModels(t.Context(), server.URL, environment.NewMapEnvProvider(nil))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Docker Desktop")
}

func TestListModels_UnreachableGateway(t *testing.T) {
	t.Parallel()

	_, err := ListModels(t.Context(), "http://127.0.0.1:1", tokenEnv())

	require.Error(t, err)
}

func TestListModels_InvalidURL(t *testing.T) {
	t.Parallel()

	_, err := ListModels(t.Context(), "http://[::1", tokenEnv())

	require.Error(t, err)
}
