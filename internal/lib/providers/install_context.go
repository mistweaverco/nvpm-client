package providers

import "strings"

// KindNeovimPlugin is stored in nvpm-lock.json extras.kind for Neovim plugins.
const KindNeovimPlugin = "neovim-plugin"

var currentInstallKind string
var currentLockedCommit string

// SetInstallKind sets the kind for the next provider Install call (e.g. KindNeovimPlugin).
func SetInstallKind(kind string) {
	currentInstallKind = kind
}

// GetInstallKind returns the install kind for the current operation.
func GetInstallKind() string {
	return currentInstallKind
}

// ResetInstallKind clears the install kind after an operation.
func ResetInstallKind() {
	currentInstallKind = ""
}

// SetLockedCommit sets the lockfile commit SHA for the next git-hosted Install/sync.
// When set, checkout uses this SHA instead of the version label (branch/tag).
func SetLockedCommit(commit string) {
	currentLockedCommit = strings.TrimSpace(commit)
}

// GetLockedCommit returns the lockfile commit SHA for the current operation.
func GetLockedCommit() string {
	return currentLockedCommit
}

// ResetLockedCommit clears the lockfile commit after an operation.
func ResetLockedCommit() {
	currentLockedCommit = ""
}

// PreferLockedGitCheckoutRef returns the ref to git-checkout. Sync pins git packages via
// lockfile commit; the version string remains the branch/tag label written back to the lock.
func PreferLockedGitCheckoutRef(version string) string {
	if c := strings.TrimSpace(GetLockedCommit()); c != "" {
		return c
	}
	return version
}
