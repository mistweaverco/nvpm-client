package providers

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitTagClobberRejections(t *testing.T) {
	out := `
From https://github.com/mistweaverco/floaterm.nvim
 ! [rejected]        v1.1.0     -> v1.1.0  (would clobber existing tag)
 * [new tag]         v9.9.9     -> v9.9.9
 ! [rejected] v1.0.0 -> v1.0.0 (would clobber existing tag)
`
	got := parseGitTagClobberRejections(out)
	assert.Equal(t, []string{"v1.1.0", "v1.0.0"}, got)
}

func TestCheckGitTagSHAAgainstDiscoveryFloatermRegression(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"

	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	err := CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit)
	require.Error(t, err)
	mismatch, ok := AsGitTagSHAMismatch(err)
	require.True(t, ok)
	assert.Equal(t, sourceID, mismatch.SourceID)
	assert.Equal(t, "v1.1.0", mismatch.Tag)
	assert.Equal(t, oldCommit, mismatch.PreviousCommit)
	assert.Equal(t, newCommit, mismatch.RemoteCommit)
	assert.Contains(t, err.Error(), "tag/release SHA mismatch")
	assert.Contains(t, err.Error(), "19198f4")
	assert.Contains(t, err.Error(), "301ea76")
	assert.Contains(t, err.Error(), "--force")
	assert.Contains(t, err.Error(), "always_trust does not bypass")

	// Same commit is fine.
	require.NoError(t, CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", oldCommit))
	// Branches are mutable and never mismatch.
	require.NoError(t, CheckGitTagSHAAgainstDiscovery(sourceID, "main", newCommit))

	// After accepting the new tip (force update), the old recording must not keep failing.
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", newCommit)))
	require.NoError(t, CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit))
}

func TestRecordingLiveTipBeforeSHACheckHidesMismatch(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	// Bug pattern: recording the live tip first makes the mismatch check pass.
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", newCommit)))
	require.NoError(t, CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit))

	// With only the old tip recorded, the mismatch is visible - enforceGitTagSHAOrReject
	// must run before enforceMinReleaseAge so we never hide this.
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))
	err := CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit)
	require.Error(t, err)
	_, ok := AsGitTagSHAMismatch(err)
	assert.True(t, ok)
}

func TestEnforceGitTagSHAOrRejectRequiresForce(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)
	ClearLastError()

	sourceID := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	old := gitDiscoveryShellOutCapture
	t.Cleanup(func() {
		gitDiscoveryShellOutCapture = old
		SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0})
	})
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 1 && args[0] == "ls-remote" {
			return 0, newCommit + "\trefs/tags/v1.1.0\n", nil
		}
		return 1, "", nil
	}

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour, Force: false})
	assert.False(t, enforceGitTagSHAOrReject(sourceID, "v1.1.0"))
	assert.Contains(t, TakeLastError(), "tag/release SHA mismatch")

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour, Force: true})
	assert.True(t, enforceGitTagSHAOrReject(sourceID, "v1.1.0"))
}

func TestAcceptGitTagSHAMismatchRecordsLiveTip(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	oldResolve := gitDiscoveryShellOutCapture
	t.Cleanup(func() { gitDiscoveryShellOutCapture = oldResolve })
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 1 && args[0] == "ls-remote" {
			return 0, newCommit + "\trefs/tags/v1.1.0\n", nil
		}
		return 1, "", nil
	}

	require.Error(t, CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit))
	acceptGitTagSHAMismatch(sourceID, "v1.1.0")
	require.NoError(t, CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit))
}

func TestGitFetchOriginTagsReportsSHAMismatch(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	oldResolve := gitDiscoveryShellOutCapture
	t.Cleanup(func() { gitDiscoveryShellOutCapture = oldResolve })

	capture := func(_ string, args []string, _ string, _ []string) (int, string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "fetch --tags origin"):
			return 1, " ! [rejected] v1.1.0     -> v1.1.0  (would clobber existing tag)\n", errors.New("exit status 1")
		case strings.HasPrefix(joined, "rev-parse v1.1.0"):
			return 0, oldCommit + "\n", nil
		case strings.Contains(joined, "ls-remote"):
			return 0, newCommit + "\trefs/tags/v1.1.0\n", nil
		default:
			return 0, "", nil
		}
	}
	gitDiscoveryShellOutCapture = capture

	err := gitFetchOriginTags(capture, "/repo", sourceID, "v1.1.0", false)
	require.Error(t, err)
	mismatch, ok := AsGitTagSHAMismatch(err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, "v1.1.0", mismatch.Tag)
	assert.True(t, gitCommitsEqual(mismatch.PreviousCommit, oldCommit))
	assert.True(t, gitCommitsEqual(mismatch.RemoteCommit, newCommit))
}

func TestGitFetchOriginTagsFallsBackForUnrelatedClobber(t *testing.T) {
	var calls []string
	capture := func(_ string, args []string, _ string, _ []string) (int, string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.HasPrefix(joined, "fetch --tags origin") {
			return 1, " ! [rejected] v1.1.0     -> v1.1.0  (would clobber existing tag)\n", errors.New("exit status 1")
		}
		if joined == "fetch origin" {
			return 0, "", nil
		}
		return 0, "", nil
	}

	err := gitFetchOriginTags(capture, "/repo", "github:owner/repo", "main", false)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "fetch --tags origin", calls[0])
	assert.Equal(t, "fetch origin", calls[1])
}

func TestGitFetchOriginTagsForceRewrites(t *testing.T) {
	var calls []string
	capture := func(_ string, args []string, _ string, _ []string) (int, string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		assert.Equal(t, "fetch --tags --force origin", joined)
		return 0, "", nil
	}
	require.NoError(t, gitFetchOriginTags(capture, "/repo", "github:owner/repo", "v1.1.0", true))
	require.Len(t, calls, 1)
}

func TestRecordGitUpdateFailureDoesNotDuplicateSHAMismatch(t *testing.T) {
	ClearLastError()
	err := &GitTagSHAMismatchError{
		SourceID:       "github:mistweaverco/floaterm.nvim",
		Tag:            "v1.1.0",
		PreviousCommit: "19198f485082474248b5919f6aa0e473a2dd9726",
		RemoteCommit:   "301ea764263d0c1a42a8fc2985047c0012347401",
	}
	recordGitUpdateFailure("GitHub Update", err)
	assert.Equal(t, err.Error(), TakeLastError())
	assert.Equal(t, "", PeekLastError())
}

func TestDiscoveredCommitsForRef(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)
	sourceID := "github:mistweaverco/floaterm.nvim"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")))
	got, err := DiscoveredCommitsForRef(sourceID, "v1.1.0")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", got[0])
}
