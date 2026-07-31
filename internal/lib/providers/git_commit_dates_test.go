package providers

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchGitCommitDateViaGit(t *testing.T) {
	oldShell := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = oldShell }()

	calls := 0
	gitDiscoveryShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		calls++
		require.Equal(t, "git", command)
		switch {
		case len(args) >= 1 && args[0] == "init":
			return 0, "", nil
		case len(args) >= 1 && args[0] == "fetch":
			return 0, "", nil
		case len(args) >= 1 && args[0] == "log":
			return 0, "2021-10-12T12:15:51-04:00\n", nil
		default:
			return 1, "unexpected", assert.AnError
		}
	}

	got, err := fetchGitCommitDateViaGit("https://github.com/tpope/vim-surround.git", "aeb9332")
	require.NoError(t, err)
	assert.Equal(t, 2021, got.Year())
	assert.GreaterOrEqual(t, calls, 3)
}

func TestFetchGitCommitDateAPIThenGitFallback(t *testing.T) {
	oldHTTP := gitCommitDateHTTPGet
	oldShell := gitDiscoveryShellOutCapture
	defer func() {
		gitCommitDateHTTPGet = oldHTTP
		gitDiscoveryShellOutCapture = oldShell
	}()

	gitCommitDateHTTPGet = func(string) (*http.Response, error) {
		return nil, assert.AnError
	}
	gitDiscoveryShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		if len(args) >= 1 && args[0] == "log" {
			return 0, "2020-01-02T03:04:05Z", nil
		}
		return 0, "", nil
	}

	got, err := FetchGitCommitDate("github:tpope/vim-surround", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	assert.True(t, got.Equal(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
}
