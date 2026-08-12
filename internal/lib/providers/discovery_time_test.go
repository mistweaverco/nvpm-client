package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordDiscoveryBatchGitUsesTagPlusCommit(t *testing.T) {
	_ = withTempNvpmHome(t)

	err := RecordDiscoveryBatch([]DiscoveryPair{{
		SourceID: "github:o/r",
		Version:  "v1.0.0",
		Commit:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}})
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(files.GetAppDataPath(), "discovery.json"))
	require.NoError(t, err)
	var db discoveryDB
	require.NoError(t, json.Unmarshal(b, &db))
	_, ok := db.FirstSeenUnix["github:o/r@v1.0.0+aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
	assert.True(t, ok)
}

func TestEnforceMinReleaseAgeGitDiscoveryKey(t *testing.T) {
	_ = withTempNvpmHome(t)
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v3.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour})
	t.Cleanup(func() { SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0}) })

	err := enforceMinReleaseAge("github:o/r", "v3.0.0")
	require.Error(t, err)
	tooSoon, ok := AsMinReleaseAgeTooSoon(err)
	require.True(t, ok)
	assert.Equal(t, "github:o/r", tooSoon.SourceID)
	assert.Equal(t, "v3.0.0+bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", tooSoon.Version)
	assert.Contains(t, err.Error(), "waiting for min-release-age")
	assert.Contains(t, err.Error(), "available in")
}

func TestRemoteLatestGetSet(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { SetDiscoveryWritesEnabled(true) })

	_, ok, err := GetRemoteLatest("github:o/plugin")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, SetRemoteLatest("github:o/plugin", RemoteLatestEntry{
		Version:          "main",
		Commit:           "cccccccccccccccccccccccccccccccccccccccc",
		SupersededTag:    "v1.2.3",
		SupersededCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SupersededUnix:   1700000000,
	}))

	entry, ok, err := GetRemoteLatest("github:o/plugin")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "main", entry.Version)
	assert.Equal(t, "cccccccccccccccccccccccccccccccccccccccc", entry.Commit)
	assert.Equal(t, "v1.2.3", entry.SupersededTag)
	assert.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", entry.SupersededCommit)
	assert.Equal(t, int64(1700000000), entry.SupersededUnix)
	assert.True(t, entry.HasSupersededTag())
	assert.Greater(t, entry.CheckedUnix, int64(0))
}

func TestHasGitCommitUpdate(t *testing.T) {
	assert.False(t, HasGitCommitUpdate("", "abc"))
	assert.False(t, HasGitCommitUpdate("abc", ""))
	assert.False(t, HasGitCommitUpdate("abc", "ABC"))
	assert.False(t, HasGitCommitUpdate("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaa"))
	assert.True(t, HasGitCommitUpdate("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
}

func TestGitCommitStillNeedsUpdateUsesLocalRemoteLatestOnly(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:mistweaverco/floaterm.nvim"
	installed := "301ea764263d0c1a42a8fc2985047c0012347401"
	stale := "19198f485082474248b5919f6aa0e473a2dd9726"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", stale)))
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", installed)))
	require.NoError(t, SetRemoteLatest(sourceID, RemoteLatestEntry{
		Version: "v1.1.0",
		Commit:  installed,
	}))

	assert.True(t, HasGitCommitUpdate(installed, stale))
	assert.False(t, GitCommitStillNeedsUpdate(sourceID, "v1.1.0", installed, stale))
	// Registry tip never recorded for this ref → still treat as an update.
	assert.True(t, GitCommitStillNeedsUpdate(sourceID, "v1.1.0", installed, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
}
