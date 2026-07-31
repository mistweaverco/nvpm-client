package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveInstalledGitRepoPath(t *testing.T) {
	root := t.TempDir()
	preferredDir := filepath.Join(root, "plugins", "github")
	alternateDir := filepath.Join(root, "packages", "github")
	safeRepo := "mistweaverco_nvpm.nvim"
	preferred := filepath.Join(preferredDir, safeRepo)
	alternate := filepath.Join(alternateDir, safeRepo)

	require.NoError(t, os.MkdirAll(preferredDir, 0755))
	require.NoError(t, os.MkdirAll(alternateDir, 0755))

	// Neither exists → preferred (new install location)
	got := resolveInstalledGitRepoPath(preferredDir, alternateDir, safeRepo, os.Stat)
	require.Equal(t, preferred, got)

	// Only preferred exists
	require.NoError(t, os.Mkdir(preferred, 0755))
	got = resolveInstalledGitRepoPath(preferredDir, alternateDir, safeRepo, os.Stat)
	require.Equal(t, preferred, got)

	// Preferred missing, alternate exists (mismatched kind / local symlink case)
	require.NoError(t, os.Remove(preferred))
	require.NoError(t, os.Mkdir(alternate, 0755))
	got = resolveInstalledGitRepoPath(preferredDir, alternateDir, safeRepo, os.Stat)
	require.Equal(t, alternate, got)

	// Both exist → preferred wins
	require.NoError(t, os.Mkdir(preferred, 0755))
	got = resolveInstalledGitRepoPath(preferredDir, alternateDir, safeRepo, os.Stat)
	require.Equal(t, preferred, got)
}
