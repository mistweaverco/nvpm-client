package providers

import (
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinReleaseAgeTooSoonErrorMessage(t *testing.T) {
	err := &MinReleaseAgeTooSoonError{
		SourceID:  "npm:eslint",
		Version:   "9.0.0",
		Age:       2 * 24 * time.Hour,
		Remaining: 5 * 24 * time.Hour,
	}
	msg := err.Error()
	assert.Contains(t, msg, "npm:eslint@9.0.0")
	assert.Contains(t, msg, "waiting for min-release-age")
	assert.Contains(t, msg, "available in 5 days")
	assert.Contains(t, msg, "--force")

	tooSoon, ok := AsMinReleaseAgeTooSoon(err)
	require.True(t, ok)
	assert.Equal(t, "npm:eslint", tooSoon.SourceID)
}

func TestFormatFriendlyDuration(t *testing.T) {
	assert.Equal(t, "1 minute", formatFriendlyDuration(30*time.Second))
	assert.Equal(t, "3 hours", formatFriendlyDuration(3*time.Hour))
	assert.Equal(t, "5 days", formatFriendlyDuration(5*24*time.Hour))
	assert.Equal(t, "2 weeks", formatFriendlyDuration(14*24*time.Hour))
}

func TestLastSkipClearsError(t *testing.T) {
	ClearLastError()
	SetLastError("boom")
	SetLastSkip("waiting")
	assert.Equal(t, "", TakeLastError())
	assert.Equal(t, "waiting", TakeLastSkip())
	assert.Equal(t, "", TakeLastSkip())
}

func TestMinReleaseAgeUpdateTargetUsesRegistryGitRefs(t *testing.T) {
	now := time.Now()
	item := registry_parser.RegistryItem{
		Version: "0.10.0",
		Source:  registry_parser.RegistryItemSource{ID: "github:mfussenegger/nvim-dap"},
		Git: &registry_parser.RegistryItemGit{
			Refs: []registry_parser.RegistryItemGitRef{
				{Ref: "0.10.0", Kind: "tag", Commit: "6a5bba0ddea5d419a783e170c20988046376090d", CommitDateUnix: now.Add(-90 * 24 * time.Hour).Unix()},
				{Ref: "master", Kind: "branch", Commit: "cfa2d58aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CommitDateUnix: now.Add(-2 * 24 * time.Hour).Unix()},
			},
		},
	}
	oldPolicy := GetPreferBranchPolicy()
	SetPreferBranchPolicy(PreferBranchPolicy{
		Branches: []string{"main", "master"},
		Kind:     config.PreferBranchWhenReleaseAgeGap,
		Gap:      60 * 24 * time.Hour,
	})
	t.Cleanup(func() { SetPreferBranchPolicy(oldPolicy) })

	version, commit := minReleaseAgeUpdateTarget("github:mfussenegger/nvim-dap", item)
	assert.Equal(t, "master", version)
	assert.Equal(t, "cfa2d58aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", commit)
}

func TestMinReleaseAgeUpdateTargetFallsBackToRemoteLatest(t *testing.T) {
	_ = withTempNvpmHome(t)
	require.NoError(t, SetRemoteLatest("github:o/manual", RemoteLatestEntry{
		Version: "main",
		Commit:  "dddddddddddddddddddddddddddddddddddddddd",
	}))

	item := registry_parser.RegistryItem{
		Version: "v1.0.0",
		Source:  registry_parser.RegistryItemSource{ID: "github:o/manual"},
	}
	version, commit := minReleaseAgeUpdateTarget("github:o/manual", item)
	assert.Equal(t, "main", version)
	assert.Equal(t, "dddddddddddddddddddddddddddddddddddddddd", commit)
}

func TestMinReleaseAgeUpdateTargetFallsBackToRegistryVersion(t *testing.T) {
	item := registry_parser.RegistryItem{
		Version: "9.0.0",
		Source:  registry_parser.RegistryItemSource{ID: "npm:eslint"},
	}
	version, commit := minReleaseAgeUpdateTarget("npm:eslint", item)
	assert.Equal(t, "9.0.0", version)
	assert.Equal(t, "", commit)
}

func TestAlreadyAtMinReleaseAgeTargetUsesCommitNotBranchName(t *testing.T) {
	_ = withTempNvpmHome(t)
	sourceID := "github:mfussenegger/nvim-dap"
	require.NoError(t, local_packages_parser.AddLocalPackageWithCommit(sourceID, "master", "oldcommitoldcommitoldcommitoldcommitold"))

	assert.False(t, alreadyAtMinReleaseAgeTarget(sourceID, "master", "cfa2d58aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	assert.True(t, alreadyAtMinReleaseAgeTarget(sourceID, "master", "oldcommitoldcommitoldcommitoldcommitold"))
	assert.False(t, alreadyAtMinReleaseAgeTarget(sourceID, "master", ""))
}

func TestEnforceMinReleaseAgeWithRegistryCommitMatchesListKey(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)
	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour})
	t.Cleanup(func() { SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0}) })

	old := gitDiscoveryShellOutCapture
	t.Cleanup(func() { gitDiscoveryShellOutCapture = old })
	gitDiscoveryShellOutCapture = func(_ string, _ []string, _ string, _ []string) (int, string, error) {
		t.Fatal("ls-remote should not run when registry already has the commit")
		return 1, "", nil
	}

	sourceID := "github:mfussenegger/nvim-dap"
	commit := "cfa2d58aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	err := enforceMinReleaseAgeWithCommit(sourceID, "master", commit)
	require.Error(t, err)
	tooSoon, ok := AsMinReleaseAgeTooSoon(err)
	require.True(t, ok)
	assert.Equal(t, FormatGitDiscoveryVersionForRef("master", commit), tooSoon.Version)
	assert.Contains(t, tooSoon.Error(), "available in")
}
