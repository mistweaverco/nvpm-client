package providers

import "strings"

// KindNeovimPlugin is stored in nvpm-lock.json extras.kind for Neovim plugins.
const KindNeovimPlugin = "neovim-plugin"

var currentInstallKind string
var lockedCommitsBySourceID = map[string]string{}

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

// SetLockedCommit records the lockfile commit SHA for a git-hosted source ID.
// Checkout uses this SHA instead of the version label (branch/tag) for that package only.
func SetLockedCommit(sourceID, commit string) {
	id := normalizePackageID(strings.TrimSpace(sourceID))
	c := strings.TrimSpace(commit)
	if id == "" {
		return
	}
	if c == "" {
		delete(lockedCommitsBySourceID, id)
		return
	}
	if lockedCommitsBySourceID == nil {
		lockedCommitsBySourceID = map[string]string{}
	}
	lockedCommitsBySourceID[id] = c
}

// GetLockedCommitFor returns the lockfile commit SHA pinned for sourceID, if any.
func GetLockedCommitFor(sourceID string) string {
	id := normalizePackageID(strings.TrimSpace(sourceID))
	if id == "" || lockedCommitsBySourceID == nil {
		return ""
	}
	return lockedCommitsBySourceID[id]
}

// ClearLockedCommit removes the pinned commit for a single source ID.
func ClearLockedCommit(sourceID string) {
	id := normalizePackageID(strings.TrimSpace(sourceID))
	if id == "" || lockedCommitsBySourceID == nil {
		return
	}
	delete(lockedCommitsBySourceID, id)
}

// ResetLockedCommit clears all pinned lockfile commits.
func ResetLockedCommit() {
	lockedCommitsBySourceID = map[string]string{}
}

// PreferLockedGitCheckoutRef returns the ref to git-checkout. Sync pins git packages via
// lockfile commit; the version string remains the branch/tag label written back to the lock.
// The SHA applies only when it was set for the same source ID.
func PreferLockedGitCheckoutRef(sourceID, version string) string {
	if c := strings.TrimSpace(GetLockedCommitFor(sourceID)); c != "" {
		return c
	}
	return version
}
