package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatGitDiscoveryVersion(t *testing.T) {
	assert.Equal(t, "v1.0.0+abc123def4567890abcdef1234567890abcdef12", FormatGitDiscoveryVersion("v1.0.0", "abc123def4567890abcdef1234567890abcdef12"))
	assert.Equal(t, "abc123def4567890abcdef1234567890abcdef12", FormatGitDiscoveryVersion("", "abc123def4567890abcdef1234567890abcdef12"))
	assert.Equal(t, "main+abc123def4567890abcdef1234567890abcdef12", FormatGitDiscoveryVersion("main", "abc123def4567890abcdef1234567890abcdef12"))
}

func TestIsGitCommitSHA(t *testing.T) {
	assert.True(t, isGitCommitSHA("abc1234"))
	assert.True(t, isGitCommitSHA("abc123def4567890abcdef1234567890abcdef12"))
	assert.False(t, isGitCommitSHA("v1.0.0"))
	assert.False(t, isGitCommitSHA("abc"))
}

func TestGitLsRemoteResolveCommitUsesSHA(t *testing.T) {
	commit, err := gitLsRemoteResolveCommit("https://example.com/a/b.git", "abc123def4567")
	require.NoError(t, err)
	assert.Equal(t, "abc123def4567", commit)
}

func TestGitLsRemoteResolveCommitFromRemote(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/tags/v1.0.0^{}" {
			return 0, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/tags/v1.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	commit, err := gitLsRemoteResolveCommit("https://github.com/o/r.git", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", commit)
}

func TestDiscoveryVersionForEnforcementGit(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" {
			return 0, "cafebabecafebabecafebabecafebabecafebabe\trefs/tags/v2.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	got, err := discoveryVersionForEnforcement("github:owner/repo", "v2.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0+cafebabecafebabecafebabecafebabecafebabe", got)

	got, err = discoveryVersionForEnforcement("npm:eslint", "9.0.0")
	require.NoError(t, err)
	assert.Equal(t, "9.0.0", got)
}

func TestEnrichDiscoveryPairGit(t *testing.T) {
	enriched, err := enrichDiscoveryPair(DiscoveryPair{
		SourceID: "github:o/r",
		Version:  "v1.0.0",
		Commit:   "1111111111111111111111111111111111111111",
	})
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0+1111111111111111111111111111111111111111", enriched.Version)

	_, err = enrichDiscoveryPair(DiscoveryPair{SourceID: "github:o/r", Version: "v1.0.0"})
	require.Error(t, err)

	enriched, err = enrichDiscoveryPair(DiscoveryPair{SourceID: "pypi:black", Version: "1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", enriched.Version)
}

func TestGitRepoURLFromSourceID(t *testing.T) {
	url, err := gitRepoURLFromSourceID("github:owner/repo")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/owner/repo.git", url)

	url, err = gitRepoURLFromSourceID("gitlab:group/project")
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/group/project.git", url)

	url, err = gitRepoURLFromSourceID("codeberg:user/pkg")
	require.NoError(t, err)
	assert.Equal(t, "https://codeberg.org/user/pkg.git", url)

	_, err = gitRepoURLFromSourceID("npm:eslint")
	require.Error(t, err)
}
