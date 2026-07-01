package nvpm

import "github.com/mistweaverco/nvpm-client/internal/lib/semver"

// chooseBestRemoteVersion picks the appropriate remote version given the
// current local version, a stable version (if any) and a prerelease version
// (if any) from the registry.
//
// Rules:
//   - If only one of stable or prerelease is set, that one is used.
//   - If currentVersion has a non-numeric prerelease (dev/alpha/beta-style),
//     we prefer the newer of (stable, prerelease) according to semver.
//   - Otherwise (local is stable or numeric-only prerelease), we prefer the
//     stable version when available; if there is no stable version, we fall
//     back to the prerelease.
func chooseBestRemoteVersion(currentVersion, stable, prerelease string) string {
	// Simple cases
	if stable == "" && prerelease == "" {
		return ""
	}
	if stable != "" && prerelease == "" {
		return stable
	}
	if stable == "" && prerelease != "" {
		return prerelease
	}

	// Both stable and prerelease exist.
	// If current is clearly on a non-numeric prerelease track,
	// pick whichever of (stable, prerelease) is greater.
	if semver.IsNonNumericPreRelease(currentVersion) {
		if semver.IsGreater(stable, prerelease) {
			// semver.IsGreater(a, b) == true means b > a
			return prerelease
		}
		return stable
	}

	// For stable or numeric prerelease locals, stick to the stable channel.
	return stable
}
