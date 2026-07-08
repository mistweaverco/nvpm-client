package providers

import "testing"

func TestFormatGitDiscoveryVersionForRefCommitOnly(t *testing.T) {
	ref := "bbbbbbb"
	commit := "bbbbbbb"
	got := FormatGitDiscoveryVersionForRef(ref, commit)
	if got != "bbbbbbb" {
		t.Fatalf("expected commit-only discovery version, got %q", got)
	}
}

func TestFormatGitDiscoveryVersionForRefTag(t *testing.T) {
	ref := "v1.2.3"
	commit := "bbbbbbb"
	got := FormatGitDiscoveryVersionForRef(ref, commit)
	if got != "v1.2.3+bbbbbbb" {
		t.Fatalf("expected tag+commit discovery version, got %q", got)
	}
}
