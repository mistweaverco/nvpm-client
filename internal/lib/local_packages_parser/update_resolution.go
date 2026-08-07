package local_packages_parser

import (
	"fmt"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
)

// ToConfig converts lock update_resolution to runtime config policy.
func (l *LockUpdateResolution) ToConfig() config.PreferBranchOverRelease {
	if l == nil {
		return config.DefaultPreferBranchOverRelease()
	}
	def := config.DefaultPreferBranchOverRelease()
	p := l.PrefersBranchOverRelease
	out := config.PreferBranchOverRelease{
		Branches: append([]string(nil), def.Branches...),
		Kind:     def.Kind,
		Gap:      def.Gap,
	}
	if len(p.Branches) > 0 {
		branches := make([]string, 0, len(p.Branches))
		for _, b := range p.Branches {
			b = strings.TrimSpace(b)
			if b != "" {
				branches = append(branches, b)
			}
		}
		if len(branches) > 0 {
			out.Branches = branches
		}
	}
	kind := config.PreferBranchWhenKind(strings.TrimSpace(p.When.Kind))
	if kind == config.PreferBranchWhenAlways || kind == config.PreferBranchWhenReleaseAgeGap {
		out.Kind = kind
	}
	if kind == config.PreferBranchWhenReleaseAgeGap {
		if gapStr := strings.TrimSpace(p.When.Gap); gapStr != "" {
			if d, err := config.ParseDuration(gapStr); err == nil && d >= 0 {
				out.Gap = d
			}
		}
	}
	return out
}

// LockUpdateResolutionFromConfig builds a lock-file update_resolution block.
func LockUpdateResolutionFromConfig(p config.PreferBranchOverRelease) *LockUpdateResolution {
	out := &LockUpdateResolution{
		PrefersBranchOverRelease: LockPrefersBranchOverRelease{
			Branches: append([]string(nil), p.Branches...),
			When: LockPreferBranchWhen{
				Kind: string(p.Kind),
			},
		},
	}
	if p.Kind == config.PreferBranchWhenReleaseAgeGap && p.Gap > 0 {
		out.PrefersBranchOverRelease.When.Gap = formatGapDuration(p.Gap)
	}
	return out
}

func formatGapDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		days := d / (24 * time.Hour)
		return fmt.Sprintf("%dd", days)
	}
	return d.String()
}
