package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreferLockedGitCheckoutRef(t *testing.T) {
	t.Cleanup(ResetLockedCommit)
	sourceID := "github:owner/repo"

	ResetLockedCommit()
	assert.Equal(t, "main", PreferLockedGitCheckoutRef(sourceID, "main"))
	assert.Equal(t, "v1.2.3", PreferLockedGitCheckoutRef(sourceID, "v1.2.3"))

	sha := "abc123def4567890abcdef1234567890abcdef12"
	SetLockedCommit(sourceID, sha)
	assert.Equal(t, sha, PreferLockedGitCheckoutRef(sourceID, "main"))
	assert.Equal(t, sha, PreferLockedGitCheckoutRef(sourceID, "v1.2.3"))

	ResetLockedCommit()
	assert.Equal(t, "main", PreferLockedGitCheckoutRef(sourceID, "main"))
}

func TestPreferLockedGitCheckoutRefScopedToSourceID(t *testing.T) {
	t.Cleanup(ResetLockedCommit)
	ResetLockedCommit()

	parent := "github:nvim-treesitter/nvim-treesitter"
	dep := "github:tree-sitter/tree-sitter-python"
	parentSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	SetLockedCommit(parent, parentSHA)

	assert.Equal(t, parentSHA, PreferLockedGitCheckoutRef(parent, "main"))
	assert.Equal(t, "master", PreferLockedGitCheckoutRef(dep, "master"))
	assert.Equal(t, "", GetLockedCommitFor(dep))

	depSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	SetLockedCommit(dep, depSHA)
	assert.Equal(t, parentSHA, PreferLockedGitCheckoutRef(parent, "main"))
	assert.Equal(t, depSHA, PreferLockedGitCheckoutRef(dep, "master"))

	ClearLockedCommit(dep)
	assert.Equal(t, parentSHA, PreferLockedGitCheckoutRef(parent, "main"))
	assert.Equal(t, "master", PreferLockedGitCheckoutRef(dep, "master"))
}

func TestGitWorkTreeExists(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, gitWorkTreeExists(""))
	assert.False(t, gitWorkTreeExists(dir))

	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	assert.True(t, gitWorkTreeExists(dir))
}
