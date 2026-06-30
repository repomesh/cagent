package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"https", "https://example.com", false},
		{"http", "http://example.com/path?q=1", false},
		{"custom scheme deep link", "docker-desktop://dashboard", false},
		{"mailto", "mailto:a@b.com", false},
		{"empty", "", true},
		{"bare path", "foo/bar", true},
		{"no scheme", "example.com", true},
		{"leading dash flag", "-foo", true},
		{"double dash flag", "--version", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validate(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
