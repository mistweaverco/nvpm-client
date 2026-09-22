package providers

import (
	"fmt"
	"strings"
)

// gitShellOutFn runs a git command; used so GitHub/GitLab/Codeberg can share checkout logic.
type gitShellOutFn func(command string, args []string, dir string, env []string) (int, error)

// gitCheckoutRef checks out ref in an already-fetched clone.
//
// For branch names, it resets the local branch to origin/<branch> (`checkout -B`) so
// updates do not leave a stale local tip after `git fetch`. Tags and commit SHAs use a
// normal checkout (detached for SHAs).
func gitCheckoutRef(shellOut gitShellOutFn, repoPath, ref string) (resolved string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty git ref")
	}
	if shellOut == nil {
		return "", fmt.Errorf("nil git shell out")
	}

	if isGitCommitSHA(ref) {
		code, err := shellOut("git", []string{"checkout", "--detach", ref}, repoPath, nil)
		if err != nil || code != 0 {
			return "", fmt.Errorf("git checkout %s: %w (exit %d)", ref, err, code)
		}
		return strings.ToLower(ref), nil
	}

	// Prefer resetting local branch to the fetched remote tip.
	remoteBranch := "origin/" + ref
	if code, verifyErr := shellOut("git", []string{"rev-parse", "--verify", "--quiet", remoteBranch}, repoPath, nil); verifyErr == nil && code == 0 {
		code, err := shellOut("git", []string{"checkout", "-B", ref, remoteBranch}, repoPath, nil)
		if err != nil || code != 0 {
			return "", fmt.Errorf("git checkout -B %s %s: %w (exit %d)", ref, remoteBranch, err, code)
		}
		return ref, nil
	}

	// Tags / other refs.
	code, err := shellOut("git", []string{"checkout", ref}, repoPath, nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("git checkout %s: %w (exit %d)", ref, err, code)
	}
	return ref, nil
}

// gitCheckoutRefWithBranchFallback tries ref, then defaultBranch when ref looks like a
// generic branch alias that may not exist on this repo.
func gitCheckoutRefWithBranchFallback(shellOut gitShellOutFn, repoPath, ref, defaultBranch string) (resolved string, err error) {
	resolved, err = gitCheckoutRef(shellOut, repoPath, ref)
	if err == nil {
		return resolved, nil
	}
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch == "" || defaultBranch == ref || !IsGenericDefaultBranchAlias(ref) {
		return "", err
	}
	return gitCheckoutRef(shellOut, repoPath, defaultBranch)
}

// gitHEADMatchesCommit reports whether repoPath is a git worktree whose HEAD equals commit.
func gitHEADMatchesCommit(repoPath, commit string) bool {
	commit = strings.TrimSpace(commit)
	if commit == "" || !gitWorkTreeExists(repoPath) {
		return false
	}
	head, err := gitRevParseHEAD(repoPath)
	if err != nil {
		return false
	}
	return gitCommitsEqual(head, commit)
}

// gitObjectExistsLocally reports whether sha is already in the local object database.
func gitObjectExistsLocally(capture gitShellOutCaptureFn, repoPath, sha string) bool {
	sha = strings.TrimSpace(sha)
	if sha == "" || repoPath == "" || capture == nil {
		return false
	}
	code, _, err := capture("git", []string{"cat-file", "-e", sha + "^{commit}"}, repoPath, nil)
	return err == nil && code == 0
}

type gitExistingWorktreeLockState int

const (
	gitLockNotPinned gitExistingWorktreeLockState = iota
	gitLockHEADMatches
	gitLockObjectLocal
	gitLockNeedsFetch
)

func gitExistingWorktreeLockStateFor(capture gitShellOutCaptureFn, sourceID, repoPath string) gitExistingWorktreeLockState {
	locked := strings.TrimSpace(GetLockedCommitFor(sourceID))
	if locked == "" {
		return gitLockNotPinned
	}
	if gitHEADMatchesCommit(repoPath, locked) {
		return gitLockHEADMatches
	}
	if gitObjectExistsLocally(capture, repoPath, locked) {
		return gitLockObjectLocal
	}
	return gitLockNeedsFetch
}

// gitFetchIfNeededForExistingClone fetches origin tags unless the locked commit is already
// HEAD (skip fetch+checkout) or already present locally (skip fetch, still checkout).
func gitFetchIfNeededForExistingClone(capture gitShellOutCaptureFn, sourceID, repoPath, version, logPrefix string) (headMatches bool, err error) {
	switch gitExistingWorktreeLockStateFor(capture, sourceID, repoPath) {
	case gitLockHEADMatches:
		Logger.Info(fmt.Sprintf("%s: already at locked commit, skipping fetch and checkout", logPrefix))
		return true, nil
	case gitLockObjectLocal:
		Logger.Info(fmt.Sprintf("%s: locked commit present locally, skipping fetch", logPrefix))
		return false, nil
	default:
		Logger.Info(fmt.Sprintf("%s: Updating repository at %s", logPrefix, repoPath))
		if err := gitFetchOriginTags(capture, repoPath, sourceID, version, allowForcedTagSHAMismatch()); err != nil {
			return false, err
		}
		return false, nil
	}
}
