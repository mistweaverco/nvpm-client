package nvpm

import (
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldShowGitDetailsSpinner(t *testing.T) {
	oldOutput := cfg.Flags.Output
	oldProgress := showGitDetailsProgress
	t.Cleanup(func() {
		cfg.Flags.Output = oldOutput
		showGitDetailsProgress = oldProgress
	})

	showGitDetailsProgress = true
	cfg.Flags.Output = config.OutputModeRich
	assert.True(t, shouldShowGitDetailsSpinner())

	cfg.Flags.Output = config.OutputModeJSON
	assert.False(t, shouldShowGitDetailsSpinner())

	showGitDetailsProgress = false
	cfg.Flags.Output = config.OutputModeRich
	assert.False(t, shouldShowGitDetailsSpinner())
}

func TestDiscoverGitRefSnapshotForShowUsesProvider(t *testing.T) {
	oldProgress := showGitDetailsProgress
	t.Cleanup(func() { showGitDetailsProgress = oldProgress })
	showGitDetailsProgress = false

	called := false
	restore := providers.SetDiscoverGitRefSnapshotForTest(func(sourceID, stableTag, prereleaseTag string) ([]providers.GitRefSnapshot, error) {
		called = true
		assert.Equal(t, "github:o/r", sourceID)
		assert.Equal(t, "v1.0.0", stableTag)
		return []providers.GitRefSnapshot{{Ref: "main", Kind: "branch", Commit: "abc"}}, nil
	})
	t.Cleanup(restore)

	snaps, err := discoverGitRefSnapshotForShow("github:o/r", "v1.0.0", "")
	require.NoError(t, err)
	require.True(t, called)
	require.Len(t, snaps, 1)
	assert.Equal(t, "main", snaps[0].Ref)
}

func TestGitRepoSpinnerLabel(t *testing.T) {
	assert.Equal(t, "saghen/blink.cmp", gitRepoSpinnerLabel("github:saghen/blink.cmp"))
	assert.Equal(t, "eslint", gitRepoSpinnerLabel("npm:eslint"))
}
