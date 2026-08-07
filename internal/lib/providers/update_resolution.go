package providers

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
)

// pendingUpdateResolution holds per-package overrides set by add/up before install.
var pendingUpdateResolution = map[string]config.PreferBranchOverRelease{}

// SetPendingUpdateResolution registers a one-shot update-resolution override for sourceID.
func SetPendingUpdateResolution(sourceID string, policy config.PreferBranchOverRelease) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	pendingUpdateResolution[sourceID] = policy
}

// ClearPendingUpdateResolution removes a pending override after install completes.
func ClearPendingUpdateResolution(sourceID string) {
	delete(pendingUpdateResolution, strings.TrimSpace(sourceID))
}

// PreferBranchPolicyForSourceID returns the effective prefer-branch policy for sourceID.
// Priority: pending CLI override → lock extras → global runtime policy.
func PreferBranchPolicyForSourceID(sourceID string) PreferBranchPolicy {
	sourceID = strings.TrimSpace(sourceID)
	if p, ok := pendingUpdateResolution[sourceID]; ok {
		return preferBranchPolicyFromConfig(p)
	}
	item := local_packages_parser.GetBySourceId(sourceID)
	if item.Extras != nil && item.Extras.UpdateResolution != nil {
		return preferBranchPolicyFromConfig(item.Extras.UpdateResolution.ToConfig())
	}
	return GetPreferBranchPolicy()
}

// ParseUpdateResolutionFlag parses compact --update-resolution values into config.
//
// Supported forms:
//   - always
//   - release-age-gap
//   - release-age-gap:30d
//   - branches:main,develop
//   - branches:main,develop;release-age-gap:30d
func ParseUpdateResolutionFlag(raw string) (config.PreferBranchOverRelease, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return config.PreferBranchOverRelease{}, fmt.Errorf("empty update-resolution value")
	}
	def := config.DefaultPreferBranchOverRelease()
	out := config.PreferBranchOverRelease{
		Branches: append([]string(nil), def.Branches...),
		Kind:     def.Kind,
		Gap:      def.Gap,
	}

	segments := strings.Split(raw, ";")
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if strings.EqualFold(seg, "always") {
			out.Kind = config.PreferBranchWhenAlways
			continue
		}
		if strings.EqualFold(seg, "release-age-gap") {
			out.Kind = config.PreferBranchWhenReleaseAgeGap
			continue
		}
		if strings.HasPrefix(strings.ToLower(seg), "release-age-gap:") {
			gapStr := strings.TrimSpace(seg[len("release-age-gap:"):])
			d, err := config.ParseDuration(gapStr)
			if err != nil || d < 0 {
				return config.PreferBranchOverRelease{}, fmt.Errorf("invalid release-age-gap duration %q", gapStr)
			}
			out.Kind = config.PreferBranchWhenReleaseAgeGap
			out.Gap = d
			continue
		}
		if strings.HasPrefix(strings.ToLower(seg), "branches:") {
			list := strings.TrimSpace(seg[len("branches:"):])
			if list == "" {
				return config.PreferBranchOverRelease{}, fmt.Errorf("branches: requires at least one branch name")
			}
			parts := strings.Split(list, ",")
			branches := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					branches = append(branches, p)
				}
			}
			if len(branches) == 0 {
				return config.PreferBranchOverRelease{}, fmt.Errorf("branches: requires at least one branch name")
			}
			out.Branches = branches
			continue
		}
		return config.PreferBranchOverRelease{}, fmt.Errorf("unknown update-resolution segment %q", seg)
	}
	if out.Kind != config.PreferBranchWhenAlways && out.Kind != config.PreferBranchWhenReleaseAgeGap {
		return config.PreferBranchOverRelease{}, fmt.Errorf("update-resolution must include always or release-age-gap")
	}
	return out, nil
}
