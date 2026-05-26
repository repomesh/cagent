package portcullistest_test

import (
	"strings"
	"testing"

	"github.com/docker/portcullis"
	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/internal/portcullistest"
)

func TestFakeGitHubPAT_DetectedByPortcullis(t *testing.T) {
	t.Parallel()

	tok := portcullistest.FakeGitHubPAT("cxLeRrvbJfmYdUtr70xnNE3Q7Gvli4")

	assert.True(t, strings.HasPrefix(tok, "ghp_"))
	assert.Len(t, tok, 40)
	assert.True(t, portcullis.Contains(tok), "synthetic PAT must trigger portcullis detection")
	assert.Equal(t, portcullis.Marker, portcullis.Redact(tok))
}

func TestFakeGitHubPAT_RejectsWrongLength(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() { portcullistest.FakeGitHubPAT("too-short") })
}
