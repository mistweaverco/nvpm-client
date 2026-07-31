package providers

import (
	"os"
	"path/filepath"
)

// resolveInstalledGitRepoPath returns the on-disk path for a git package.
// It prefers preferred (kind-derived: plugins/ vs packages/). If that path is
// missing but alternate exists - e.g. a neovim-plugin locked under packages/,
// or a legacy install - it returns alternate. When neither exists, preferred
// is returned so new installs land in the correct directory.
func resolveInstalledGitRepoPath(preferredDir, alternateDir, safeRepo string, stat func(string) (os.FileInfo, error)) string {
	preferred := filepath.Join(preferredDir, safeRepo)
	if _, err := stat(preferred); err == nil {
		return preferred
	}
	alternate := filepath.Join(alternateDir, safeRepo)
	if _, err := stat(alternate); err == nil {
		return alternate
	}
	return preferred
}
