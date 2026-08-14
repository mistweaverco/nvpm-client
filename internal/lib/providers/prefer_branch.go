package providers

import (
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
)

// PreferBranchPolicy mirrors config.PreferBranchOverRelease for use during discovery/install.
type PreferBranchPolicy struct {
	Branches []string
	Kind     config.PreferBranchWhenKind
	Gap      time.Duration
}

var preferBranchPolicy = preferBranchPolicyFromConfig(config.DefaultPreferBranchOverRelease())

func preferBranchPolicyFromConfig(p config.PreferBranchOverRelease) PreferBranchPolicy {
	return PreferBranchPolicy{
		Branches: append([]string(nil), p.Branches...),
		Kind:     p.Kind,
		Gap:      p.Gap,
	}
}

// SetPreferBranchPolicy sets the runtime prefer-branch-over-release policy.
func SetPreferBranchPolicy(p PreferBranchPolicy) {
	if len(p.Branches) == 0 {
		p.Branches = config.DefaultPreferBranchOverRelease().Branches
	}
	if p.Kind == "" {
		p.Kind = config.PreferBranchWhenReleaseAgeGap
	}
	if p.Kind == config.PreferBranchWhenReleaseAgeGap && p.Gap <= 0 {
		p.Gap = config.DefaultPreferBranchOverRelease().Gap
	}
	preferBranchPolicy = p
}

// SetPreferBranchPolicyFromConfig applies a config.PreferBranchOverRelease value.
func SetPreferBranchPolicyFromConfig(p config.PreferBranchOverRelease) {
	SetPreferBranchPolicy(preferBranchPolicyFromConfig(p))
}

// GetPreferBranchPolicy returns the current prefer-branch policy.
func GetPreferBranchPolicy() PreferBranchPolicy {
	out := preferBranchPolicy
	out.Branches = append([]string(nil), preferBranchPolicy.Branches...)
	return out
}

// IsPreferBranchRef reports whether ref is a configured preferred branch (or a generic
// default-branch alias such as main/master).
func IsPreferBranchRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if IsGenericDefaultBranchAlias(ref) {
		return true
	}
	for _, b := range GetPreferBranchPolicy().Branches {
		if strings.EqualFold(strings.TrimSpace(b), ref) {
			return true
		}
	}
	return false
}

// PreferRemoteLatestOverRegistry reports whether cached remote_latest should override
// registry versions for update/list. Packages not in the registry always use the cache.
// In-registry packages only use it when the cache is a prefer-branch ref (e.g. main),
// not a competing semver tag (e.g. stale v1.0.0 vs registry v0.25.0).
func PreferRemoteLatestOverRegistry(entry RemoteLatestEntry, registryStable, registryPrerelease string) bool {
	if strings.TrimSpace(entry.Version) == "" {
		return false
	}
	if strings.TrimSpace(registryStable) == "" && strings.TrimSpace(registryPrerelease) == "" {
		return true
	}
	return IsPreferBranchRef(entry.Version)
}

// PreferBranchDecision is the pure policy outcome given tag/branch candidates and upstream dates.
type PreferBranchDecision struct {
	UseBranch bool
	Reason    string
}

// DecidePreferBranch applies the prefer-branch-over-release policy.
//
// now is used to measure tag age for release-age-gap.
// tagTime / branchTime are upstream timestamps; zero means unknown.
// hasTag / hasBranch indicate whether a candidate exists.
func DecidePreferBranch(
	policy PreferBranchPolicy,
	now time.Time,
	hasTag bool,
	tagTime time.Time,
	hasBranch bool,
	branchTime time.Time,
) PreferBranchDecision {
	if !hasBranch {
		return PreferBranchDecision{UseBranch: false, Reason: "no preferred branch"}
	}
	if policy.Kind == config.PreferBranchWhenAlways {
		return PreferBranchDecision{UseBranch: true, Reason: "always"}
	}
	// release-age-gap
	if !hasTag {
		return PreferBranchDecision{UseBranch: true, Reason: "no tag"}
	}
	if tagTime.IsZero() || branchTime.IsZero() {
		// Fail closed toward keeping the tag when dates are unavailable.
		return PreferBranchDecision{UseBranch: false, Reason: "missing upstream dates"}
	}
	tagAge := now.Sub(tagTime)
	if tagAge < policy.Gap {
		return PreferBranchDecision{UseBranch: false, Reason: "tag within gap"}
	}
	if !branchTime.After(tagTime) {
		return PreferBranchDecision{UseBranch: false, Reason: "branch not newer than tag"}
	}
	return PreferBranchDecision{UseBranch: true, Reason: "release-age-gap"}
}
