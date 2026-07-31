package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// gitCommitDateHTTPGet is overridable in tests.
var gitCommitDateHTTPGet = http.Get

// FetchGitCommitDate returns the upstream committer/author date for a commit SHA on a git host.
// Tries the host HTTP API first, then falls back to a shallow `git fetch` of the commit
// (needed when APIs are rate-limited or blocked).
func FetchGitCommitDate(sourceID, commitSHA string) (time.Time, error) {
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return time.Time{}, fmt.Errorf("empty commit SHA")
	}
	normalized := normalizePackageID(sourceID)
	providerName, repo := extractProviderAndPackage(normalized)
	if repo == "" {
		return time.Time{}, fmt.Errorf("invalid source id %q", sourceID)
	}

	var apiErr error
	switch strings.ToLower(providerName) {
	case "github":
		if t, err := fetchGitHubCommitDate(repo, commitSHA); err == nil {
			return t, nil
		} else {
			apiErr = err
		}
	case "gitlab":
		if t, err := fetchGitLabCommitDate(repo, commitSHA); err == nil {
			return t, nil
		} else {
			apiErr = err
		}
	case "codeberg":
		if t, err := fetchCodebergCommitDate(repo, commitSHA); err == nil {
			return t, nil
		} else {
			apiErr = err
		}
	default:
		return time.Time{}, fmt.Errorf("unsupported git host: %s", providerName)
	}

	repoURL, err := gitRepoURLFromSourceID(sourceID)
	if err != nil {
		if apiErr != nil {
			return time.Time{}, fmt.Errorf("%w (git fallback unavailable: %v)", apiErr, err)
		}
		return time.Time{}, err
	}
	t, gitErr := fetchGitCommitDateViaGit(repoURL, commitSHA)
	if gitErr != nil {
		if apiErr != nil {
			return time.Time{}, fmt.Errorf("api: %v; git: %w", apiErr, gitErr)
		}
		return time.Time{}, gitErr
	}
	return t, nil
}

// fetchGitCommitDateViaGit shallow-fetches a single commit and reads its committer date.
func fetchGitCommitDateViaGit(repoURL, sha string) (time.Time, error) {
	repoURL = strings.TrimSpace(repoURL)
	sha = strings.TrimSpace(sha)
	if repoURL == "" || sha == "" {
		return time.Time{}, fmt.Errorf("empty repo URL or SHA")
	}
	tmp, err := os.MkdirTemp("", "nvpm-commit-date-*")
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if code, _, err := gitDiscoveryShellOutCapture("git", []string{"init", "--quiet"}, tmp, nil); err != nil || code != 0 {
		return time.Time{}, fmt.Errorf("git init: %w", err)
	}
	// Fetch the exact commit (works for tags and branch tips resolved to SHAs).
	if code, out, err := gitDiscoveryShellOutCapture("git", []string{"fetch", "--quiet", "--depth", "1", repoURL, sha}, tmp, nil); err != nil || code != 0 {
		return time.Time{}, fmt.Errorf("git fetch %s: %v (%s)", sha, err, strings.TrimSpace(out))
	}
	code, out, err := gitDiscoveryShellOutCapture("git", []string{"log", "-1", "--format=%cI", "FETCH_HEAD"}, tmp, nil)
	if err != nil || code != 0 {
		// Fallback: try the SHA directly after fetch.
		code, out, err = gitDiscoveryShellOutCapture("git", []string{"log", "-1", "--format=%cI", sha}, tmp, nil)
		if err != nil || code != 0 {
			return time.Time{}, fmt.Errorf("git log date: %v (%s)", err, strings.TrimSpace(out))
		}
	}
	return parseRFC3339Flex(strings.TrimSpace(out))
}

func fetchGitHubCommitDate(repo, sha string) (time.Time, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repo, url.PathEscape(sha))
	resp, err := gitCommitDateHTTPGet(apiURL)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("GitHub commit API status %d", resp.StatusCode)
	}
	var body struct {
		Commit struct {
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
			Author struct {
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return time.Time{}, err
	}
	if t, err := parseRFC3339Flex(body.Commit.Committer.Date); err == nil {
		return t, nil
	}
	return parseRFC3339Flex(body.Commit.Author.Date)
}

func fetchGitLabCommitDate(repo, sha string) (time.Time, error) {
	encoded := url.PathEscape(repo)
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/commits/%s", encoded, url.PathEscape(sha))
	resp, err := gitCommitDateHTTPGet(apiURL)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("GitLab commit API status %d", resp.StatusCode)
	}
	var body struct {
		CommittedDate string `json:"committed_date"`
		CreatedAt     string `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return time.Time{}, err
	}
	if t, err := parseRFC3339Flex(body.CommittedDate); err == nil {
		return t, nil
	}
	return parseRFC3339Flex(body.CreatedAt)
}

func fetchCodebergCommitDate(repo, sha string) (time.Time, error) {
	apiURL := fmt.Sprintf("https://codeberg.org/api/v1/repos/%s/git/commits/%s", repo, url.PathEscape(sha))
	resp, err := gitCommitDateHTTPGet(apiURL)
	if err != nil {
		return time.Time{}, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		apiURL = fmt.Sprintf("https://codeberg.org/api/v1/repos/%s/commits/%s", repo, url.PathEscape(sha))
		resp, err = gitCommitDateHTTPGet(apiURL)
		if err != nil {
			return time.Time{}, err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("Codeberg commit API status %d", resp.StatusCode)
	}
	var body struct {
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
			Author struct {
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Created time.Time `json:"created"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return time.Time{}, err
	}
	if !body.Commit.Committer.Date.IsZero() {
		return body.Commit.Committer.Date, nil
	}
	if !body.Commit.Author.Date.IsZero() {
		return body.Commit.Author.Date, nil
	}
	if !body.Created.IsZero() {
		return body.Created, nil
	}
	return time.Time{}, fmt.Errorf("no date in Codeberg commit response")
}

func parseRFC3339Flex(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	// git %cI can include a timezone offset like -04:00 (RFC3339).
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Tolerate trailing newlines from command output.
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return parseRFC3339Flex(s[:i])
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q", s)
}

// fetchGitCommitDateFn is overridable in tests for DiscoverGitRemoteLatest.
var fetchGitCommitDateFn = FetchGitCommitDate
