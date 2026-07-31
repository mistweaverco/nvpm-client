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
