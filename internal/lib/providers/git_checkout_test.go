package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitCheckoutRefResetsBranchToOrigin(t *testing.T) {
	var calls [][]string
	shell := func(_ string, args []string, _ string, _ []string) (int, error) {
		calls = append(calls, append([]string{}, args...))
		return 0, nil
	}

	got, err := gitCheckoutRef(shell, "/repo", "main")
	require.NoError(t, err)
	assert.Equal(t, "main", got)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"rev-parse", "--verify", "--quiet", "origin/main"}, calls[0])
	assert.Equal(t, []string{"checkout", "-B", "main", "origin/main"}, calls[1])
}

func TestGitCheckoutRefDetachSHA(t *testing.T) {
	var calls [][]string
	shell := func(_ string, args []string, _ string, _ []string) (int, error) {
		calls = append(calls, append([]string{}, args...))
		return 0, nil
	}

	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err := gitCheckoutRef(shell, "/repo", sha)
	require.NoError(t, err)
	assert.Equal(t, sha, got)
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"checkout", "--detach", sha}, calls[0])
}

func TestGitCheckoutRefFallsBackToPlainCheckoutForTags(t *testing.T) {
	var calls [][]string
	shell := func(_ string, args []string, _ string, _ []string) (int, error) {
		calls = append(calls, append([]string{}, args...))
		if len(args) >= 1 && args[0] == "rev-parse" {
			return 1, nil // no origin/v1.0.0
		}
		return 0, nil
	}

	got, err := gitCheckoutRef(shell, "/repo", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", got)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"checkout", "v1.0.0"}, calls[1])
}

func TestGitCheckoutRefWithBranchFallback(t *testing.T) {
	shell := func(_ string, args []string, _ string, _ []string) (int, error) {
		if len(args) >= 4 && args[0] == "rev-parse" && args[3] == "origin/main" {
			return 1, nil
		}
		if len(args) >= 4 && args[0] == "rev-parse" && args[3] == "origin/master" {
			return 0, nil
		}
		if len(args) >= 1 && args[0] == "checkout" && args[1] == "main" {
			return 1, assert.AnError
		}
		return 0, nil
	}

	got, err := gitCheckoutRefWithBranchFallback(shell, "/repo", "main", "master")
	require.NoError(t, err)
	assert.Equal(t, "master", got)
}

func TestGitFetchIfNeededForExistingCloneSkipsWhenHEADMatches(t *testing.T) {
	t.Cleanup(ResetLockedCommit)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	stubGitRevParseHEAD(t, testLockSHA, nil)
	SetLockedCommit("github:owner/repo", testLockSHA)

	capture := func(string, []string, string, []string) (int, string, error) {
		t.Fatal("fetch should not run when HEAD matches lock")
		return 1, "", assert.AnError
	}
	headMatches, err := gitFetchIfNeededForExistingClone(capture, "github:owner/repo", dir, "main", "test")
	require.NoError(t, err)
	assert.True(t, headMatches)
}

func TestGitFetchIfNeededForExistingCloneSkipsFetchWhenObjectLocal(t *testing.T) {
	t.Cleanup(ResetLockedCommit)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	stubGitRevParseHEAD(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil)
	SetLockedCommit("github:owner/repo", testLockSHA)

	var calls [][]string
	capture := func(_ string, args []string, _ string, _ []string) (int, string, error) {
		calls = append(calls, append([]string{}, args...))
		if len(args) >= 1 && args[0] == "cat-file" {
			return 0, "", nil
		}
		t.Fatalf("unexpected git %v", args)
		return 1, "", assert.AnError
	}
	headMatches, err := gitFetchIfNeededForExistingClone(capture, "github:owner/repo", dir, "main", "test")
	require.NoError(t, err)
	assert.False(t, headMatches)
	require.Len(t, calls, 1)
	assert.Equal(t, "cat-file", calls[0][0])
}
