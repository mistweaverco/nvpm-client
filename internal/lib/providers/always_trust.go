package providers

import (
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
)

// pendingAlwaysTrust holds per-package always_trust set by add/up before the lock is written.
var pendingAlwaysTrust = map[string]bool{}

// SetPendingAlwaysTrust registers a one-shot always_trust override for sourceID during install.
func SetPendingAlwaysTrust(sourceID string, trust bool) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	pendingAlwaysTrust[sourceID] = trust
}

// ClearPendingAlwaysTrust removes a pending always_trust override after install completes.
func ClearPendingAlwaysTrust(sourceID string) {
	delete(pendingAlwaysTrust, strings.TrimSpace(sourceID))
}

// PackageAlwaysTrust reports whether sourceID should skip min-release-age.
// Priority: pending CLI override → lock extras.always_trust.
func PackageAlwaysTrust(sourceID string) bool {
	sourceID = strings.TrimSpace(sourceID)
	if trust, ok := pendingAlwaysTrust[sourceID]; ok {
		return trust
	}
	item := local_packages_parser.GetBySourceId(sourceID)
	return item.Extras != nil && item.Extras.AlwaysTrust
}
