package githubauth

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

const userAgent = "nvpm"

var (
	tokenMu     sync.Mutex
	tokenCached bool
	cachedToken string

	ghAuthTokenFn = defaultGhAuthToken
	httpDo        = func(req *http.Request) (*http.Response, error) {
		return http.DefaultClient.Do(req)
	}
)

// Token returns a GitHub token, resolved once per process in this order:
// GH_TOKEN, GITHUB_TOKEN, then `gh auth token` when gh is available.
func Token() string {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if !tokenCached {
		cachedToken = resolveToken()
		tokenCached = true
	}
	return cachedToken
}

// ResetCache clears the cached token. Tests use this after changing env or stubs.
func ResetCache() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	tokenCached = false
	cachedToken = ""
}

func resolveToken() string {
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t
	}
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(ghAuthTokenFn())
}

func defaultGhAuthToken() string {
	code, out, err := shell_out.ShellOutCapture("gh", []string{"auth", "token"}, "", nil)
	if err != nil || code != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}

// HTTPGet is a drop-in for http.Get that identifies nvpm and authenticates
// requests to api.github.com and github.com when a token is available.
func HTTPGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if isGitHubHost(req.URL.Host) {
		if token := Token(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return httpDo(req)
}

// APIStatusError explains GitHub API failures, including rate-limit hints for 403/429.
func APIStatusError(status int) error {
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return fmt.Errorf("GitHub API returned status %d (unauthenticated requests are rate-limited; set GH_TOKEN or authenticate with gh)", status)
	}
	return fmt.Errorf("GitHub API returned status %d", status)
}

func isGitHubHost(host string) bool {
	host = strings.ToLower(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = strings.ToLower(h)
	}
	return host == "api.github.com" || host == "github.com"
}
