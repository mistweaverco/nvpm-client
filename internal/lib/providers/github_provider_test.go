package providers

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubGitHubRegistryJSON(t *testing.T, rawArray string) {
	t.Helper()
	old := githubRegistryParser
	t.Cleanup(func() { githubRegistryParser = old })
	data := []byte(rawArray)
	githubRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: data})
		require.NoError(t, p.LoadFromBytes(data))
		return p
	}
}

func TestGitHubUpdateReleaseAssetsSkipsGitFetchWhenMissing(t *testing.T) {
	_ = withTempNvpmHome(t)
	ClearLastError()
	t.Cleanup(ClearLastError)

	stubGitHubRegistryJSON(t, `[{
		"name": "harper-ls",
		"version": "v2.8.0",
		"source": {
			"id": "github:elijah-potter/harper",
			"asset": [{"file": "harper-ls.tar.gz", "bin": "harper-ls"}]
		}
	}]`)

	oldCapture := githubShellOutCapture
	oldHTTP := githubHTTPGet
	oldDiscover := discoverGitRemoteLatestFn
	t.Cleanup(func() {
		githubShellOutCapture = oldCapture
		githubHTTPGet = oldHTTP
		discoverGitRemoteLatestFn = oldDiscover
	})
	discoverGitRemoteLatestFn = func(string, string) (GitRemoteLatestResult, error) {
		return GitRemoteLatestResult{}, errors.New("offline")
	}
	gitCalled := false
	githubShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if cmd == "git" {
			gitCalled = true
		}
		return 1, "not a git repository", errors.New("git should not run")
	}
	githubHTTPGet = func(string) (*http.Response, error) {
		return nil, errors.New("download stub")
	}

	got := githubRegistryParser().GetBySourceId("github:elijah-potter/harper")
	require.Equal(t, "github:elijah-potter/harper", got.Source.ID)
	require.NotEmpty(t, got.Source.Asset)

	p := NewProviderGitHub()
	ok := p.Update("github:elijah-potter/harper")
	assert.False(t, ok)
	assert.False(t, gitCalled, "release-asset updates must not git fetch")
	assert.NotContains(t, TakeLastError(), "is not installed")
}

func TestGitHubUpdateGitCloneMissingStillReportsNotInstalled(t *testing.T) {
	_ = withTempNvpmHome(t)
	ClearLastError()
	t.Cleanup(ClearLastError)

	stubGitHubRegistryJSON(t, `[{
		"name": "some-plugin",
		"version": "v1.0.0",
		"source": {"id": "github:owner/missing-clone"}
	}]`)

	p := NewProviderGitHub()
	ok := p.Update("github:owner/missing-clone")
	assert.False(t, ok)
	assert.Contains(t, TakeLastError(), "is not installed")
}

func TestGitHubUpdateReleaseAssetsSkipsGitFetchWhenDirExists(t *testing.T) {
	_ = withTempNvpmHome(t)
	ClearLastError()
	t.Cleanup(ClearLastError)

	stubGitHubRegistryJSON(t, `[{
		"name": "ols",
		"version": "dev-2026-08",
		"source": {
			"id": "github:DanielGavin/ols",
			"asset": [{"file": "ols.zip", "bin": "ols"}]
		}
	}]`)

	p := NewProviderGitHub()
	repoPath := p.getRepoPath("github:DanielGavin/ols", "DanielGavin/ols")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))

	oldCapture := githubShellOutCapture
	oldHTTP := githubHTTPGet
	oldDiscover := discoverGitRemoteLatestFn
	t.Cleanup(func() {
		githubShellOutCapture = oldCapture
		githubHTTPGet = oldHTTP
		discoverGitRemoteLatestFn = oldDiscover
	})
	discoverGitRemoteLatestFn = func(string, string) (GitRemoteLatestResult, error) {
		return GitRemoteLatestResult{}, errors.New("offline")
	}
	gitCalled := false
	githubShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if cmd == "git" {
			gitCalled = true
		}
		return 1, "not a git repository", nil
	}
	githubHTTPGet = func(string) (*http.Response, error) {
		return nil, errors.New("download stub")
	}

	ok := p.Update("github:DanielGavin/ols")
	assert.False(t, ok)
	assert.False(t, gitCalled, "release-asset updates must not git fetch")
}

func TestGitHubInstallFromReleaseKeepsNightlyTag(t *testing.T) {
	_ = withTempNvpmHome(t)
	ClearLastError()
	t.Cleanup(ClearLastError)

	stubGitHubRegistryJSON(t, `[{
		"name": "ols",
		"source": {
			"id": "github:DanielGavin/ols",
			"asset": [{"file": "ols.zip", "bin": "ols"}]
		}
	}]`)

	oldHTTP := githubHTTPGet
	t.Cleanup(func() { githubHTTPGet = oldHTTP })
	var gotURL string
	githubHTTPGet = func(url string) (*http.Response, error) {
		gotURL = url
		return nil, errors.New("download stub")
	}

	ok := NewProviderGitHub().Install("github:DanielGavin/ols", "nightly")
	assert.False(t, ok)
	assert.Contains(t, gotURL, "/DanielGavin/ols/releases/download/nightly/")
	assert.NotContains(t, gotURL, "/releases/download/latest/")
}

func TestGitHubInstallFromReleaseMapsMainToLatest(t *testing.T) {
	_ = withTempNvpmHome(t)
	ClearLastError()
	t.Cleanup(ClearLastError)

	stubGitHubRegistryJSON(t, `[{
		"name": "ols",
		"version": "dev-2026-06",
		"source": {
			"id": "github:DanielGavin/ols",
			"asset": [{"file": "ols.zip", "bin": "ols"}]
		}
	}]`)

	oldHTTP := githubHTTPGet
	t.Cleanup(func() { githubHTTPGet = oldHTTP })
	var gotURL string
	githubHTTPGet = func(url string) (*http.Response, error) {
		gotURL = url
		return nil, errors.New("download stub")
	}

	ok := NewProviderGitHub().Install("github:DanielGavin/ols", "main")
	assert.False(t, ok)
	assert.Contains(t, gotURL, "/DanielGavin/ols/releases/download/dev-2026-06/")
}

func TestResolveReleaseUpdateVersionPrefersGitHubRelease(t *testing.T) {
	_ = withTempNvpmHome(t)
	got, err := resolveReleaseUpdateVersion("github:DanielGavin/ols", "nightly", func() (string, error) {
		return "dev-2026-06", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "dev-2026-06", got)
}

func TestResolveReleaseUpdateVersionFallsBackToRegistry(t *testing.T) {
	_ = withTempNvpmHome(t)
	got, err := resolveReleaseUpdateVersion("github:DanielGavin/ols", "dev-2026-05", func() (string, error) {
		return "", errors.New("offline")
	})
	require.NoError(t, err)
	assert.Equal(t, "dev-2026-05", got)
}

func TestResolveReleaseUpdateVersionFallsBackToAPI(t *testing.T) {
	_ = withTempNvpmHome(t)
	got, err := resolveReleaseUpdateVersion("github:elijah-potter/harper", "", func() (string, error) {
		return "v2.8.0", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "v2.8.0", got)
}

func TestResolveReleaseUpdateVersionSkipsNightlyRegistryVersion(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldDiscover := discoverGitRemoteLatestFn
	t.Cleanup(func() { discoverGitRemoteLatestFn = oldDiscover })
	discoverGitRemoteLatestFn = func(string, string) (GitRemoteLatestResult, error) {
		return GitRemoteLatestResult{Version: "nightly"}, nil
	}
	_, err := resolveReleaseUpdateVersion("github:DanielGavin/ols", "nightly", func() (string, error) {
		return "", errors.New("offline")
	})
	require.Error(t, err)
}
