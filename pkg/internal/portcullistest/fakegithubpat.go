// Package portcullistest provides test helpers for code that
// exercises portcullis' GitHub-token detection rules.
package portcullistest

import "hash/crc32"

// FakeGitHubPAT returns a synthetic GitHub PAT whose trailing 6-char
// base62 CRC32 suffix passes portcullis' validGitHubChecksum, so the
// returned token is detected (and redacted) by the secret scanner.
//
// The token is assembled at runtime: the full 40-char `ghp_…` string
// never appears as a source literal, so GitHub's secret-scanning
// push protection won't flag files that call this helper.
//
// body must be exactly 30 base62 characters; callers vary it to get
// distinct fake tokens. The result has the GitHub-PAT shape
// `ghp_` + 36 base62 chars and is never a real credential.
func FakeGitHubPAT(body string) string {
	const (
		alphabet  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
		suffixLen = 6
	)
	if len(body) != 30 {
		panic("portcullistest: body must be 30 chars")
	}
	var suffix [suffixLen]byte
	checksum := uint64(crc32.ChecksumIEEE([]byte(body)))
	for i := suffixLen - 1; i >= 0; i-- {
		suffix[i] = alphabet[checksum%62]
		checksum /= 62
	}
	return "ghp_" + body + string(suffix[:])
}
