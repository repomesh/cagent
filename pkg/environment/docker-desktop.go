package environment

import (
	"context"
	"net/url"
	"strings"

	"github.com/docker/docker-agent/pkg/desktop"
)

const (
	DockerDesktopEmail    = "DOCKER_EMAIL"
	DockerDesktopUsername = "DOCKER_USERNAME"
	DockerDesktopTokenEnv = "DOCKER_TOKEN"
)

// IsTrustedDockerURL checks if the URL targets a domain trusted to receive
// the Docker Desktop JWT. It matches:
//   - "docker.com" and any subdomain (e.g. "desktop.docker.com")
//   - localhost addresses ("localhost", "127.0.0.1", "::1") for local development
//
// It performs strict hostname validation to prevent token leakage.
func IsTrustedDockerURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "docker.com" || strings.HasSuffix(host, ".docker.com") ||
		host == "localhost" || host == "127.0.0.1" || host == "::1"
}

type DockerDesktopProvider struct{}

func NewDockerDesktopProvider() *DockerDesktopProvider {
	return &DockerDesktopProvider{}
}

func (p *DockerDesktopProvider) Get(ctx context.Context, name string) (string, bool) {
	switch name {
	case DockerDesktopEmail:
		return desktop.GetUserInfo(ctx).Email, true

	case DockerDesktopUsername:
		return desktop.GetUserInfo(ctx).Username, true

	case DockerDesktopTokenEnv:
		return desktop.GetToken(ctx), true

	default:
		return "", false
	}
}
