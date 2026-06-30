package tui

import "testing"

func TestExpandSessionPlaceholder(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		url       string
		sessionID string
		want      string
	}{
		{
			name:      "no placeholder is unchanged",
			url:       "https://example.com/feedback",
			sessionID: "abc123",
			want:      "https://example.com/feedback",
		},
		{
			name:      "query placeholder substituted",
			url:       "https://example.com/feedback?session={{session_id}}",
			sessionID: "abc123",
			want:      "https://example.com/feedback?session=abc123",
		},
		{
			name:      "custom scheme deep link",
			url:       "docker-desktop://dashboard/session/{{session_id}}",
			sessionID: "abc123",
			want:      "docker-desktop://dashboard/session/abc123",
		},
		{
			name:      "multiple occurrences",
			url:       "https://x/{{session_id}}?s={{session_id}}",
			sessionID: "abc123",
			want:      "https://x/abc123?s=abc123",
		},
		{
			name:      "session id is query-escaped",
			url:       "https://example.com/?s={{session_id}}",
			sessionID: "a b&c=d",
			want:      "https://example.com/?s=a+b%26c%3Dd",
		},
		{
			name:      "empty session id yields empty value",
			url:       "https://example.com/?s={{session_id}}",
			sessionID: "",
			want:      "https://example.com/?s=",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := expandSessionPlaceholder(tt.url, tt.sessionID)
			if got != tt.want {
				t.Fatalf("expandSessionPlaceholder(%q, %q) = %q, want %q", tt.url, tt.sessionID, got, tt.want)
			}
		})
	}
}
