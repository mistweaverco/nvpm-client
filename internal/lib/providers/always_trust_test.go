package providers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceMinReleaseAgeBypassedByPendingAlwaysTrust(t *testing.T) {
	_ = withTempNvpmHome(t)

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour})
	t.Cleanup(func() {
		SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0})
		ClearPendingAlwaysTrust("npm:eslint")
	})

	SetDiscoveryWritesEnabled(true)
	require.NoError(t, RecordDiscoveryBatch([]DiscoveryPair{{
		SourceID: "npm:eslint",
		Version:  "9.0.0",
	}}))

	err := enforceMinReleaseAge("npm:eslint", "9.0.0")
	require.Error(t, err)

	SetPendingAlwaysTrust("npm:eslint", true)
	err = enforceMinReleaseAge("npm:eslint", "9.0.0")
	assert.NoError(t, err)
}

func TestAlwaysTrustDoesNotBypassTagSHAMismatch(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"
	require.NoError(t, RecordDiscovery(sourceID, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	SetPendingAlwaysTrust(sourceID, true)
	t.Cleanup(func() {
		ClearPendingAlwaysTrust(sourceID)
		SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0})
	})

	err := CheckGitTagSHAAgainstDiscovery(sourceID, "v1.1.0", newCommit)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "always_trust does not bypass")

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour, Force: false})
	assert.False(t, allowForcedTagSHAMismatch(), "always_trust must not imply --force for tag SHA checks")
	assert.True(t, PackageAlwaysTrust(sourceID))

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour, Force: true})
	assert.True(t, allowForcedTagSHAMismatch())
}

func TestAlwaysTrustStillRecordsDiscoveryForGitTips(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)

	sourceID := "github:owner/plugin"
	commit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	SetPendingAlwaysTrust(sourceID, true)
	t.Cleanup(func() {
		ClearPendingAlwaysTrust(sourceID)
		SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0})
	})

	old := gitDiscoveryShellOutCapture
	t.Cleanup(func() { gitDiscoveryShellOutCapture = old })
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 1 && args[0] == "ls-remote" {
			return 0, commit + "\trefs/tags/v1.0.0\n", nil
		}
		return 1, "", nil
	}

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour})
	require.NoError(t, enforceMinReleaseAge(sourceID, "v1.0.0"))

	prevs, err := DiscoveredCommitsForRef(sourceID, "v1.0.0")
	require.NoError(t, err)
	require.Len(t, prevs, 1)
	assert.Equal(t, commit, prevs[0])

	// A later tip move is visible as a SHA mismatch even with always_trust.
	err = CheckGitTagSHAAgainstDiscovery(sourceID, "v1.0.0", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.Error(t, err)
	_, ok := AsGitTagSHAMismatch(err)
	assert.True(t, ok)
}
