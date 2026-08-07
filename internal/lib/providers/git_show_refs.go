package providers

import (
	"fmt"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

var trackedGitShowBranches = []string{"main", "master", "develop"}

// GitRefSnapshot is a branch or tag tip with optional upstream commit date.
type GitRefSnapshot struct {
	Ref            string
	Kind           string // "branch" | "tag"
	Commit         string
	CommitDateUnix int64
}

type gitRefCandidate struct {
	ref    string
	kind   string
	commit string
}

// DiscoverGitRefSnapshot fetches tracked branch tips and semver tags for nvpm show.
// Unlike list commands, show may perform network I/O when registry git metadata is absent.
func DiscoverGitRefSnapshot(sourceID, stableTag, prereleaseTag string) ([]GitRefSnapshot, error) {
	return discoverGitRefSnapshotFn(sourceID, stableTag, prereleaseTag)
}

func discoverGitRefSnapshot(sourceID, stableTag, prereleaseTag string) ([]GitRefSnapshot, error) {
	if !IsGitHostedSourceID(sourceID) {
		return nil, fmt.Errorf("not a git-hosted source id: %s", sourceID)
	}
	repoURL, err := gitRepoURLFromSourceID(sourceID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var candidates []gitRefCandidate
	add := func(c gitRefCandidate) {
		if c.ref == "" || c.commit == "" {
			return
		}
		key := c.kind + ":" + strings.ToLower(c.ref)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, c)
	}

	for _, branch := range trackedGitShowBranches {
		commit, resolveErr := gitLsRemoteResolveCommit(repoURL, branch)
		if resolveErr != nil || commit == "" {
			continue
		}
		add(gitRefCandidate{ref: branch, kind: "branch", commit: commit})
	}

	var tagListing string
	if code, out, lsErr := gitDiscoveryShellOutCapture("git", []string{"ls-remote", "--tags", repoURL}, "", nil); lsErr == nil && code == 0 {
		tagListing = out
	}

	stable := strings.TrimSpace(stableTag)
	if stable == "" && tagListing != "" {
		if tag, ok := pickLatestSemverTag(tagListing); ok {
			stable = tag
		}
	}
	if stable != "" {
		if commit, resolveErr := gitLsRemoteResolveCommit(repoURL, stable); resolveErr == nil && commit != "" {
			add(gitRefCandidate{ref: stable, kind: "tag", commit: commit})
		}
	}

	prerelease := strings.TrimSpace(prereleaseTag)
	if prerelease != "" && !strings.EqualFold(prerelease, stable) {
		if commit, resolveErr := gitLsRemoteResolveCommit(repoURL, prerelease); resolveErr == nil && commit != "" {
			add(gitRefCandidate{ref: prerelease, kind: "tag", commit: commit})
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no git refs resolved for %s", sourceID)
	}

	out := make([]GitRefSnapshot, 0, len(candidates))
	for _, c := range candidates {
		snap := GitRefSnapshot{
			Ref:    c.ref,
			Kind:   c.kind,
			Commit: strings.ToLower(c.commit),
		}
		if t, dateErr := fetchGitCommitDateFn(sourceID, c.commit); dateErr == nil && !t.IsZero() {
			snap.CommitDateUnix = t.Unix()
		}
		out = append(out, snap)
	}
	return out, nil
}

// GitRefsFromSnapshots converts live ref snapshots into registry-shaped git refs.
func GitRefsFromSnapshots(snaps []GitRefSnapshot) []registry_parser.RegistryItemGitRef {
	out := make([]registry_parser.RegistryItemGitRef, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, registry_parser.RegistryItemGitRef{
			Ref:            s.Ref,
			Kind:           s.Kind,
			Commit:         s.Commit,
			CommitDateUnix: s.CommitDateUnix,
		})
	}
	return out
}

// discoverGitRefSnapshotFn is overridable in tests.
var discoverGitRefSnapshotFn = discoverGitRefSnapshot

// SetDiscoverGitRefSnapshotForTest overrides live ref discovery for show.
func SetDiscoverGitRefSnapshotForTest(fn func(sourceID, stableTag, prereleaseTag string) ([]GitRefSnapshot, error)) (restore func()) {
	prev := discoverGitRefSnapshotFn
	if fn == nil {
		discoverGitRefSnapshotFn = discoverGitRefSnapshot
	} else {
		discoverGitRefSnapshotFn = fn
	}
	return func() { discoverGitRefSnapshotFn = prev }
}

// RefSnapshotCommitTime returns the commit time for a snapshot entry.
func RefSnapshotCommitTime(s GitRefSnapshot) time.Time {
	if s.CommitDateUnix <= 0 {
		return time.Time{}
	}
	return time.Unix(s.CommitDateUnix, 0)
}
