package providers

import (
	"fmt"
	"regexp"
	"strings"
)

// gitShellOutCaptureFn runs a git command and returns exit code, combined output, and error.
type gitShellOutCaptureFn func(command string, args []string, dir string, env []string) (int, string, error)

var gitTagClobberRe = regexp.MustCompile(`(?m)^\s*!\s*\[rejected\]\s+(\S+)\s+->\s+\S+\s+\(would clobber existing tag\)`)

// GitTagSHAMismatchError is returned when a previously recorded tag/release points at a
// different commit than the live remote (typically a force-moved tag).
type GitTagSHAMismatchError struct {
	SourceID       string
	Tag            string
	PreviousCommit string
	RemoteCommit   string
}

func (e *GitTagSHAMismatchError) Error() string {
	prev := shortCommitForError(e.PreviousCommit)
	remote := shortCommitForError(e.RemoteCommit)
	return fmt.Sprintf(
		"tag/release SHA mismatch for %s@%s: previously recorded %s, remote now %s (upstream tag was force-moved). Refusing to update; re-run with --force to accept the new commit (always_trust does not bypass tag SHA checks).",
		e.SourceID, e.Tag, prev, remote,
	)
}

// AsGitTagSHAMismatch reports whether err is (or wraps) a tag SHA mismatch.
func AsGitTagSHAMismatch(err error) (*GitTagSHAMismatchError, bool) {
	if err == nil {
		return nil, false
	}
	if e, ok := err.(*GitTagSHAMismatchError); ok {
		return e, true
	}
	return nil, false
}

func shortCommitForError(commit string) string {
	commit = strings.TrimSpace(strings.ToLower(commit))
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func gitCommitsEqual(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	// Allow short/full prefix matches.
	if len(a) >= 7 && len(b) >= 7 {
		if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
			return true
		}
	}
	return false
}

func isMutableGitRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || isGitCommitSHA(ref) {
		return true
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

// DiscoveredCommitsForRef returns previously recorded commits for a git tag/branch ref.
func DiscoveredCommitsForRef(sourceID, ref string) ([]string, error) {
	sourceID = strings.TrimSpace(sourceID)
	ref = strings.TrimSpace(ref)
	if sourceID == "" || ref == "" {
		return nil, nil
	}
	vers, err := ListDiscoveredVersions(sourceID)
	if err != nil {
		return nil, err
	}
	prefix := ref + "+"
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, dv := range vers {
		ver := strings.TrimSpace(dv.Version)
		if !strings.HasPrefix(ver, prefix) {
			continue
		}
		commit := strings.TrimSpace(strings.TrimPrefix(ver, prefix))
		if commit == "" {
			continue
		}
		key := strings.ToLower(commit)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, commit)
	}
	return out, nil
}

// CheckGitTagSHAMismatchLive resolves the live remote commit for ref and fails when
// discovery previously recorded the same tag/release at a different SHA.
func CheckGitTagSHAMismatchLive(sourceID, ref string) error {
	sourceID = strings.TrimSpace(sourceID)
	ref = strings.TrimSpace(ref)
	if !IsGitHostedSourceID(sourceID) || isMutableGitRef(ref) {
		return nil
	}
	live, err := ResolveGitDiscoveryCommit(sourceID, ref)
	if err != nil || strings.TrimSpace(live) == "" {
		// Cannot verify live tip; do not block on network/ref resolution failures here.
		return nil
	}
	return CheckGitTagSHAAgainstDiscovery(sourceID, ref, live)
}

// CheckGitTagSHAAgainstDiscovery fails when discovery has previously recorded this
// tag/release, but never at the current remote commit (force-moved to an unseen tip).
// Once the new tip has been accepted (recorded), subsequent checks succeed even if an
// older tip for the same tag remains in discovery history.
func CheckGitTagSHAAgainstDiscovery(sourceID, ref, remoteCommit string) error {
	sourceID = strings.TrimSpace(sourceID)
	ref = strings.TrimSpace(ref)
	remoteCommit = strings.TrimSpace(remoteCommit)
	if !IsGitHostedSourceID(sourceID) || isMutableGitRef(ref) || remoteCommit == "" {
		return nil
	}
	prevs, err := DiscoveredCommitsForRef(sourceID, ref)
	if err != nil || len(prevs) == 0 {
		return nil
	}
	for _, prev := range prevs {
		if gitCommitsEqual(prev, remoteCommit) {
			return nil
		}
	}
	return &GitTagSHAMismatchError{
		SourceID:       sourceID,
		Tag:            ref,
		PreviousCommit: prevs[0],
		RemoteCommit:   remoteCommit,
	}
}

// acceptGitTagSHAMismatch records the live tip for ref so a force-accepted
// force-moved tag does not keep failing on subsequent updates.
func acceptGitTagSHAMismatch(sourceID, ref string) {
	sourceID = strings.TrimSpace(sourceID)
	ref = strings.TrimSpace(ref)
	if !IsGitHostedSourceID(sourceID) || isMutableGitRef(ref) {
		return
	}
	live, err := ResolveGitDiscoveryCommit(sourceID, ref)
	if err != nil || strings.TrimSpace(live) == "" {
		return
	}
	if err := RecordDiscovery(sourceID, FormatGitDiscoveryVersionForRef(ref, live)); err != nil {
		Logger.Info(fmt.Sprintf("warning: could not record accepted tag tip for %s@%s: %v", sourceID, ref, err))
	}
	RefreshRemoteLatestAfterInstall(sourceID, ref, live)
}

// parseGitTagClobberRejections extracts tag names from `git fetch --tags` rejection output.
func parseGitTagClobberRejections(output string) []string {
	matches := gitTagClobberRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		tag := strings.TrimSpace(m[1])
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func localTagCommit(capture gitShellOutCaptureFn, repoPath, tag string) string {
	if capture == nil || strings.TrimSpace(repoPath) == "" || strings.TrimSpace(tag) == "" {
		return ""
	}
	code, out, err := capture("git", []string{"rev-parse", tag}, repoPath, nil)
	if err != nil || code != 0 {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(out))
}

// gitFetchOriginTags updates remote refs/tags. When force is false, force-moved tags are
// reported as GitTagSHAMismatchError when they match targetRef (or when targetRef is empty).
// Unrelated rejected tags fall back to a non-tag fetch so branch updates can still proceed.
func gitFetchOriginTags(capture gitShellOutCaptureFn, repoPath, sourceID, targetRef string, force bool) error {
	if capture == nil {
		return fmt.Errorf("nil git shell out capture")
	}
	args := []string{"fetch", "--tags", "origin"}
	if force {
		args = []string{"fetch", "--tags", "--force", "origin"}
	}
	code, out, err := capture("git", args, repoPath, nil)
	if err == nil && code == 0 {
		return nil
	}
	rejected := parseGitTagClobberRejections(out)
	if len(rejected) > 0 && !force {
		targetRef = strings.TrimSpace(targetRef)
		var blocking []string
		for _, tag := range rejected {
			if targetRef == "" || strings.EqualFold(tag, targetRef) {
				blocking = append(blocking, tag)
			}
		}
		if len(blocking) > 0 {
			tag := blocking[0]
			local := localTagCommit(capture, repoPath, tag)
			remote, _ := ResolveGitDiscoveryCommit(sourceID, tag)
			if local == "" {
				if prevs, discErr := DiscoveredCommitsForRef(sourceID, tag); discErr == nil && len(prevs) > 0 {
					local = prevs[0]
				}
			}
			if local != "" && remote != "" && !gitCommitsEqual(local, remote) {
				return &GitTagSHAMismatchError{
					SourceID:       sourceID,
					Tag:            tag,
					PreviousCommit: local,
					RemoteCommit:   remote,
				}
			}
			detail := strings.TrimSpace(out)
			if detail == "" {
				detail = "would clobber existing tag"
			}
			return fmt.Errorf(
				"tag/release SHA mismatch for %s@%s: git refused to update the local tag (%s). Re-run with --force to accept the remote tag, or set the version explicitly via `nvpm add %s@%s`",
				sourceID, tag, detail, sourceID, tag,
			)
		}
		// Target is not among the force-moved tags (e.g. updating a branch). Fetch tips without
		// rewriting local tags so the update can proceed.
		Logger.Info(fmt.Sprintf(
			"git fetch --tags skipped force-moved tag(s) %s while updating %s@%s",
			strings.Join(rejected, ", "), sourceID, targetRef,
		))
		code, out, err = capture("git", []string{"fetch", "origin"}, repoPath, nil)
		if err == nil && code == 0 {
			return nil
		}
		detail := strings.TrimSpace(out)
		if detail != "" {
			return fmt.Errorf("git fetch failed: %s", detail)
		}
		if err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
		return fmt.Errorf("git fetch failed: exit %d", code)
	}
	detail := strings.TrimSpace(out)
	if detail != "" {
		return fmt.Errorf("git fetch --tags failed: %s", detail)
	}
	if err != nil {
		return fmt.Errorf("git fetch --tags failed: %w", err)
	}
	return fmt.Errorf("git fetch --tags failed: exit %d", code)
}

// enforceGitTagSHAOrReject blocks install/update when a tag/release tip was force-moved
// relative to discovery history, unless --force was passed. Sync (locked commit) skips this.
// Returns false when the operation must abort (LastError already set).
func enforceGitTagSHAOrReject(sourceID, version string) bool {
	if strings.TrimSpace(GetLockedCommit()) != "" {
		return true
	}
	if !IsGitHostedSourceID(sourceID) {
		return true
	}
	ref := strings.TrimSpace(version)
	if ref == "" || ref == "latest" {
		return true
	}
	if err := CheckGitTagSHAMismatchLive(sourceID, ref); err != nil {
		if !allowForcedTagSHAMismatch() {
			SetLastError(err.Error())
			return false
		}
		Logger.Info(fmt.Sprintf("%v (--force accepting new commit)", err))
		acceptGitTagSHAMismatch(sourceID, ref)
	}
	return true
}

// allowForcedTagSHAMismatch reports whether CLI --force should accept force-moved tags.
// always_trust must never satisfy this - it only skips min-release-age, not tag SHA checks.
func allowForcedTagSHAMismatch() bool {
	return minReleaseAgePolicy.Force
}

// recordGitUpdateFailure records a git install/update failure for the CLI.
// Tag SHA mismatches are user-facing (shown via TakeLastError) and must not also
// emit slog ERROR when debug is off - that duplicated the message under `nvpm up`.
func recordGitUpdateFailure(prefix string, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if _, ok := AsGitTagSHAMismatch(err); ok || strings.Contains(msg, "tag/release SHA mismatch") {
		SetLastError(msg)
		return
	}
	logAndSetError(fmt.Sprintf("%s: %v", prefix, err))
}
