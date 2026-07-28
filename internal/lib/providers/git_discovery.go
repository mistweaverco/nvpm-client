package providers

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

var gitDiscoveryShellOutCapture = shell_out.ShellOutCapture

// IsGitHostedProvider reports whether packages from this provider are resolved from a git host.
func IsGitHostedProvider(p Provider) bool {
	return p == ProviderGitHub || p == ProviderGitLab || p == ProviderCodeberg
}

// IsGitHostedSourceID reports whether sourceID refers to a git-hosted package.
func IsGitHostedSourceID(sourceID string) bool {
	return IsGitHostedProvider(detectProvider(sourceID))
}

// FormatGitDiscoveryVersion builds the discovery-time version string for git packages.
// When a tag/release is known, records "tag+commit"; otherwise records the commit SHA alone.
func FormatGitDiscoveryVersion(tag, commit string) string {
	tag = strings.TrimSpace(tag)
	commit = strings.TrimSpace(commit)
	if tag != "" && commit != "" {
		return tag + "+" + commit
	}
	if commit != "" {
		return commit
	}
	return tag
}

// FormatGitDiscoveryVersionForRef formats the discovery-time version string from a user-facing ref
// (tag/branch/SHA) plus a resolved commit SHA.
//
// - For commit-only refs, this returns the commit SHA (not "sha+sha").
// - For tag/branch refs, this returns "ref+commit".
func FormatGitDiscoveryVersionForRef(ref, commit string) string {
	return FormatGitDiscoveryVersion(gitTagForDiscoveryRef(ref), strings.TrimSpace(commit))
}

func isGitCommitSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

func gitRepoURLFromSourceID(sourceID string) (string, error) {
	normalized := normalizePackageID(sourceID)
	providerName, repo := extractProviderAndPackage(normalized)
	if repo == "" {
		return "", fmt.Errorf("invalid git source id %q", sourceID)
	}
	switch strings.ToLower(providerName) {
	case "github":
		return fmt.Sprintf("https://github.com/%s.git", repo), nil
	case "gitlab":
		return fmt.Sprintf("https://gitlab.com/%s.git", repo), nil
	case "codeberg":
		return fmt.Sprintf("https://codeberg.org/%s.git", repo), nil
	default:
		return "", fmt.Errorf("not a git-hosted provider: %s", sourceID)
	}
}

func parseLsRemoteFirstCommit(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 1 && len(fields[0]) >= 7 {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func parseLsRemoteDefaultBranch(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: ") {
			continue
		}
		rest := strings.TrimPrefix(line, "ref: ")
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		ref := fields[0]
		if strings.HasPrefix(ref, "refs/heads/") {
			return strings.TrimPrefix(ref, "refs/heads/")
		}
	}
	return ""
}

func gitLocalDefaultBranch(repoPath string) (string, bool) {
	if strings.TrimSpace(repoPath) == "" {
		return "", false
	}
	code, branchOutput, err := gitDiscoveryShellOutCapture("git", []string{"symbolic-ref", "refs/remotes/origin/HEAD"}, repoPath, nil)
	if err == nil && code == 0 {
		branch := strings.TrimSpace(branchOutput)
		if strings.HasPrefix(branch, "refs/remotes/origin/") {
			return strings.TrimPrefix(branch, "refs/remotes/origin/"), true
		}
	}
	for _, branch := range []string{"main", "master", "trunk", "release"} {
		code, _, _ := gitDiscoveryShellOutCapture("git", []string{"show-ref", "--verify", "--quiet", "refs/remotes/origin/" + branch}, repoPath, nil)
		if code == 0 {
			return branch, true
		}
	}
	return "", false
}

func gitLsRemoteDefaultBranch(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("empty repository URL")
	}
	code, out, err := gitDiscoveryShellOutCapture("git", []string{"ls-remote", "--symref", repoURL, "HEAD"}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("git ls-remote --symref: %w", err)
	}
	if branch := parseLsRemoteDefaultBranch(out); branch != "" {
		return branch, nil
	}
	return "", fmt.Errorf("no default branch in ls-remote output")
}

// IsGenericDefaultBranchAlias reports common placeholder branch names that may not
// exist on a given repository (for example when we previously defaulted to "main").
func IsGenericDefaultBranchAlias(ref string) bool {
	switch strings.ToLower(strings.TrimSpace(ref)) {
	case "main", "master", "trunk", "head":
		return true
	default:
		return false
	}
}

// ResolveGitDefaultBranch resolves the repository default branch from a local clone
// and/or remote URL. Falls back to "main" when discovery fails.
func ResolveGitDefaultBranch(repoURL, repoPath string) string {
	if branch, ok := gitLocalDefaultBranch(repoPath); ok {
		return branch
	}
	if strings.TrimSpace(repoURL) != "" {
		if branch, err := gitLsRemoteDefaultBranch(repoURL); err == nil && branch != "" {
			return branch
		}
	}
	return "main"
}

func gitLsRemoteResolveCommit(repoURL, ref string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	ref = strings.TrimSpace(ref)
	if repoURL == "" {
		return "", fmt.Errorf("empty repository URL")
	}
	if ref == "" {
		return "", fmt.Errorf("empty git ref")
	}
	if isGitCommitSHA(ref) {
		return strings.ToLower(ref), nil
	}

	candidates := []string{
		"refs/tags/" + ref + "^{}",
		"refs/tags/" + ref,
		"refs/heads/" + ref,
	}
	for _, candidate := range candidates {
		code, out, err := gitDiscoveryShellOutCapture("git", []string{"ls-remote", repoURL, candidate}, "", nil)
		if err != nil || code != 0 {
			continue
		}
		if commit := parseLsRemoteFirstCommit(out); commit != "" {
			return commit, nil
		}
	}
	return "", fmt.Errorf("cannot resolve git ref %q in %s", ref, repoURL)
}

// ResolveGitDiscoveryCommit resolves a git tag/branch/ref to a full commit SHA for discovery recording.
func ResolveGitDiscoveryCommit(sourceID, ref string) (string, error) {
	repoURL, err := gitRepoURLFromSourceID(sourceID)
	if err != nil {
		return "", err
	}
	return gitLsRemoteResolveCommit(repoURL, ref)
}

func gitTagForDiscoveryRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || isGitCommitSHA(ref) {
		return ""
	}
	return ref
}

func discoveryVersionForEnforcement(sourceID, version string) (string, error) {
	if !IsGitHostedSourceID(sourceID) {
		return version, nil
	}
	repoURL, err := gitRepoURLFromSourceID(sourceID)
	if err != nil {
		return "", err
	}
	commit, err := gitLsRemoteResolveCommit(repoURL, version)
	if err != nil {
		return "", err
	}
	return FormatGitDiscoveryVersion(gitTagForDiscoveryRef(version), commit), nil
}

func enrichDiscoveryPair(p DiscoveryPair) (DiscoveryPair, error) {
	if !IsGitHostedSourceID(p.SourceID) {
		return p, nil
	}
	if strings.Contains(p.Version, "+") {
		return p, nil
	}
	tag := strings.TrimSpace(p.Version)
	commit := strings.TrimSpace(p.Commit)
	if commit == "" {
		// Git discovery keys require a commit. Callers must supply one (e.g. from the
		// lockfile); we intentionally avoid git ls-remote here because list commands
		// would fan out thousands of network calls. Install/update resolves commits
		// via discoveryVersionForEnforcement instead.
		return DiscoveryPair{}, fmt.Errorf("git discovery pair missing commit for %s@%s", p.SourceID, tag)
	}
	p.Version = FormatGitDiscoveryVersion(gitTagForDiscoveryRef(tag), commit)
	p.Commit = ""
	return p, nil
}

func persistGitHostedPackage(sourceID, tag, repoPath, repoURL string) error {
	tag = strings.TrimSpace(tag)
	var commit string
	var err error
	if strings.TrimSpace(repoPath) != "" {
		commit, err = gitRevParseHEAD(repoPath)
	}
	if err != nil || commit == "" {
		if strings.TrimSpace(repoURL) != "" && tag != "" {
			commit, err = gitLsRemoteResolveCommit(repoURL, tag)
		}
	}
	if err != nil {
		return err
	}
	if commit == "" {
		return fmt.Errorf("cannot resolve commit for %s@%s", sourceID, tag)
	}
	return local_packages_parser.AddLocalPackageWithCommit(sourceID, tag, commit)
}

func gitRevParseHEAD(dir string) (string, error) {
	return defaultExternalQueriesGitRevParse(dir)
}
