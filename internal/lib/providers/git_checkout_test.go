package providers

import (
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
