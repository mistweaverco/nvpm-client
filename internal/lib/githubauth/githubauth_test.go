package githubauth

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateToken(t *testing.T) {
	t.Helper()
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	oldGh := ghAuthTokenFn
	ghAuthTokenFn = func() string { return "" }
	t.Cleanup(func() { ghAuthTokenFn = oldGh })
	ResetCache()
	t.Cleanup(ResetCache)
}

func TestTokenGHTokenTakesPrecedence(t *testing.T) {
	isolateToken(t)
	t.Setenv("GH_TOKEN", "from-gh-token")
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	called := false
	ghAuthTokenFn = func() string {
		called = true
		return "from-gh-cli"
	}
	ResetCache()

	assert.Equal(t, "from-gh-token", Token())
	assert.False(t, called)
}

func TestTokenGITHUBTokenWhenGHTokenEmpty(t *testing.T) {
	isolateToken(t)
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	called := false
	ghAuthTokenFn = func() string {
		called = true
		return "from-gh-cli"
	}
	ResetCache()

	assert.Equal(t, "from-github-token", Token())
	assert.False(t, called)
}

func TestTokenFallsBackToGhAuthToken(t *testing.T) {
	isolateToken(t)
	ghAuthTokenFn = func() string { return "from-gh-cli\n" }
	ResetCache()

	assert.Equal(t, "from-gh-cli", Token())
}

func TestTokenCachesGhLookup(t *testing.T) {
	isolateToken(t)
	calls := 0
	ghAuthTokenFn = func() string {
		calls++
		return "cached"
	}
	ResetCache()

	assert.Equal(t, "cached", Token())
	assert.Equal(t, "cached", Token())
	assert.Equal(t, 1, calls)
}

func TestTokenEmptyWhenNothingConfigured(t *testing.T) {
	isolateToken(t)
	assert.Equal(t, "", Token())
}

func TestHTTPGetSetsAuthorizationForGitHubHosts(t *testing.T) {
	isolateToken(t)
	t.Setenv("GH_TOKEN", "secret-token")
	ResetCache()

	var got *http.Request
	oldDo := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		got = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	t.Cleanup(func() { httpDo = oldDo })

	resp, err := HTTPGet("https://api.github.com/repos/mistweaverco/nvpm-client/releases/latest")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	require.NotNil(t, got)
	assert.Equal(t, "Bearer secret-token", got.Header.Get("Authorization"))
	assert.Equal(t, userAgent, got.Header.Get("User-Agent"))

	resp, err = HTTPGet("https://github.com/mistweaverco/nvpm-client/releases/download/v1.0.0/nvpm")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, "Bearer secret-token", got.Header.Get("Authorization"))
}

func TestHTTPGetOmitsAuthorizationWithoutToken(t *testing.T) {
	isolateToken(t)

	var got *http.Request
	oldDo := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		got = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	t.Cleanup(func() { httpDo = oldDo })

	resp, err := HTTPGet("https://api.github.com/repos/foo/bar/releases/latest")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	require.NotNil(t, got)
	assert.Empty(t, got.Header.Get("Authorization"))
	assert.Equal(t, userAgent, got.Header.Get("User-Agent"))
}

func TestHTTPGetDoesNotAttachTokenToNonGitHubHosts(t *testing.T) {
	isolateToken(t)
	t.Setenv("GH_TOKEN", "secret-token")
	ResetCache()

	var got *http.Request
	oldDo := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		got = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	t.Cleanup(func() { httpDo = oldDo })

	resp, err := HTTPGet("https://gitlab.com/api/v4/projects/foo/repository/commits/abc")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	require.NotNil(t, got)
	assert.Empty(t, got.Header.Get("Authorization"))
	assert.Equal(t, userAgent, got.Header.Get("User-Agent"))
}

func TestAPIStatusError(t *testing.T) {
	err := APIStatusError(http.StatusForbidden)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "GH_TOKEN")
	assert.Contains(t, err.Error(), "gh")

	err = APIStatusError(http.StatusTooManyRequests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "GH_TOKEN")

	err = APIStatusError(http.StatusNotFound)
	require.Error(t, err)
	assert.Equal(t, "GitHub API returned status 404", err.Error())
}

func TestIsGitHubHost(t *testing.T) {
	assert.True(t, isGitHubHost("api.github.com"))
	assert.True(t, isGitHubHost("github.com"))
	assert.True(t, isGitHubHost("API.GITHUB.COM"))
	assert.True(t, isGitHubHost("github.com:443"))
	assert.False(t, isGitHubHost("gitlab.com"))
	assert.False(t, isGitHubHost("codeberg.org"))
	assert.False(t, isGitHubHost("gist.github.com"))
}
