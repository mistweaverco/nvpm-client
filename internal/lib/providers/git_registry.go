package providers

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

var semverTagRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

func registryRefByName(refs []registry_parser.RegistryItemGitRef, name string) (registry_parser.RegistryItemGitRef, bool) {
	name = strings.TrimSpace(name)
	for _, r := range refs {
		if strings.EqualFold(strings.TrimSpace(r.Ref), name) {
			return r, true
		}
	}
	return registry_parser.RegistryItemGitRef{}, false
}

func parseSemverTag(name string) (major, minor, patch int, ok bool) {
	m := semverTagRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return 0, 0, 0, false
	}
	return atoiSemver(m[1]), atoiSemver(m[2]), atoiSemver(m[3]), true
}

func atoiSemver(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func compareSemverTags(a, b string) int {
	ma, mi, pa, oka := parseSemverTag(a)
	mb, mj, pb, okb := parseSemverTag(b)
	if !oka && !okb {
		return strings.Compare(a, b)
	}
	if !oka {
		return -1
	}
	if !okb {
		return 1
	}
	if ma != mb {
		return ma - mb
	}
	if mi != mj {
		return mi - mj
	}
	return pa - pb
}

func latestSemverTagRef(refs []registry_parser.RegistryItemGitRef) (registry_parser.RegistryItemGitRef, bool) {
	var best registry_parser.RegistryItemGitRef
	found := false
	for _, r := range refs {
		if r.Kind != "tag" {
			continue
		}
		if _, _, _, ok := parseSemverTag(r.Ref); !ok {
			continue
		}
		if !found || compareSemverTags(r.Ref, best.Ref) > 0 {
			best = r
			found = true
		}
	}
	return best, found
}

func pickBranchFromRegistryRefs(refs []registry_parser.RegistryItemGitRef, branches []string) (name, commit string, branchTime time.Time, ok bool) {
	type candidate struct {
		name   string
		commit string
		date   time.Time
	}
	var found []candidate
	for _, b := range branches {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if r, exists := registryRefByName(refs, b); exists && r.Kind == "branch" && strings.TrimSpace(r.Commit) != "" {
			found = append(found, candidate{
				name:   r.Ref,
				commit: strings.TrimSpace(r.Commit),
				date:   r.CommitTime(),
			})
		}
	}
	if len(found) == 0 {
		return "", "", time.Time{}, false
	}
	best := found[0]
	for _, c := range found[1:] {
		switch {
		case !c.date.IsZero() && !best.date.IsZero():
			if c.date.After(best.date) {
				best = c
			}
		case !c.date.IsZero() && best.date.IsZero():
			best = c
		}
	}
	return best.name, best.commit, best.date, true
}

// ResolveGitLatestFromRegistry applies prefer-branch-over-release using registry git.refs.
// Returns ok=false when the item has no git metadata.
func ResolveGitLatestFromRegistry(item registry_parser.RegistryItem, policy PreferBranchPolicy) (GitRemoteLatestResult, PreferBranchDecision, bool) {
	if item.Git == nil || len(item.Git.Refs) == 0 {
		return GitRemoteLatestResult{}, PreferBranchDecision{}, false
	}
	refs := item.Git.Refs
	now := time.Now()

	var (
		tagRef registry_parser.RegistryItemGitRef
		hasTag bool
	)
	if v := strings.TrimSpace(item.Version); v != "" && !IsGitCommitHash(v) {
		if r, ok := registryRefByName(refs, v); ok && r.Kind == "tag" {
			tagRef, hasTag = r, true
		}
	}
	if !hasTag {
		tagRef, hasTag = latestSemverTagRef(refs)
	}

	tagTime := tagRef.CommitTime()
	branchName, branchCommit, branchTime, hasBranch := pickBranchFromRegistryRefs(refs, policy.Branches)

	decision := DecidePreferBranch(policy, now, hasTag, tagTime, hasBranch, branchTime)

	if decision.UseBranch && hasBranch {
		out := GitRemoteLatestResult{Version: branchName, Commit: branchCommit}
		if hasTag && decision.Reason == "release-age-gap" && !tagTime.IsZero() {
			out.SupersededTag = tagRef.Ref
			out.SupersededCommit = tagRef.Commit
			out.SupersededTimeUnix = tagTime.Unix()
		}
		return out, decision, true
	}
	if hasTag {
		return GitRemoteLatestResult{Version: tagRef.Ref, Commit: tagRef.Commit}, decision, true
	}
	if hasBranch {
		return GitRemoteLatestResult{Version: branchName, Commit: branchCommit}, decision, true
	}
	if v := strings.TrimSpace(item.Version); v != "" {
		if r, ok := registryRefByName(refs, v); ok {
			return GitRemoteLatestResult{Version: r.Ref, Commit: r.Commit}, decision, true
		}
		return GitRemoteLatestResult{Version: v, Commit: ""}, decision, true
	}
	return GitRemoteLatestResult{}, decision, false
}

// ResolveGitLatestRefForItem resolves latest ref using registry git metadata when present.
func ResolveGitLatestRefForItem(item registry_parser.RegistryItem, policy PreferBranchPolicy) (GitRemoteLatestResult, error) {
	if result, _, ok := ResolveGitLatestFromRegistry(item, policy); ok {
		if strings.TrimSpace(result.Version) == "" {
			return GitRemoteLatestResult{}, fmt.Errorf("cannot resolve latest ref for %s", item.Source.ID)
		}
		return result, nil
	}
	if v := strings.TrimSpace(item.Version); v != "" && v != "unknown" {
		return GitRemoteLatestResult{Version: v}, nil
	}
	return GitRemoteLatestResult{}, fmt.Errorf("cannot resolve latest ref for %s", item.Source.ID)
}
