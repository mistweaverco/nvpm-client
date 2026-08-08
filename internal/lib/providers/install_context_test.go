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

	ResetLockedCommit()
	assert.Equal(t, "main", PreferLockedGitCheckoutRef("main"))
	assert.Equal(t, "v1.2.3", PreferLockedGitCheckoutRef("v1.2.3"))

	SetLockedCommit("abc123def4567890abcdef1234567890abcdef12")
	assert.Equal(t, "abc123def4567890abcdef1234567890abcdef12", PreferLockedGitCheckoutRef("main"))
	assert.Equal(t, "abc123def4567890abcdef1234567890abcdef12", PreferLockedGitCheckoutRef("v1.2.3"))

	ResetLockedCommit()
	assert.Equal(t, "main", PreferLockedGitCheckoutRef("main"))
}

func TestGitWorkTreeExists(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, gitWorkTreeExists(""))
	assert.False(t, gitWorkTreeExists(dir))

	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	assert.True(t, gitWorkTreeExists(dir))
}
