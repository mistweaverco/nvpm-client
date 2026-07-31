package providers

import (
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatGitDiscoveryVersion(t *testing.T) {
	assert.Equal(t, "v1.0.0+abc123def4567890abcdef1234567890abcdef12", FormatGitDiscoveryVersion("v1.0.0", "abc123def4567890abcdef1234567890abcdef12"))
	assert.Equal(t, "abc123def4567890abcdef1234567890abcdef12", FormatGitDiscoveryVersion("", "abc123def4567890abcdef1234567890abcdef12"))
	assert.Equal(t, "main+abc123def4567890abcdef1234567890abcdef12", FormatGitDiscoveryVersion("main", "abc123def4567890abcdef1234567890abcdef12"))
}

func TestIsGitCommitSHA(t *testing.T) {
	assert.True(t, isGitCommitSHA("abc1234"))
	assert.True(t, isGitCommitSHA("abc123def4567890abcdef1234567890abcdef12"))
	assert.False(t, isGitCommitSHA("v1.0.0"))
	assert.False(t, isGitCommitSHA("abc"))
}

func TestGitLsRemoteResolveCommitUsesSHA(t *testing.T) {
	commit, err := gitLsRemoteResolveCommit("https://example.com/a/b.git", "abc123def4567")
	require.NoError(t, err)
	assert.Equal(t, "abc123def4567", commit)
}

func TestGitLsRemoteResolveCommitFromRemote(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/tags/v1.0.0^{}" {
			return 0, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/tags/v1.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	commit, err := gitLsRemoteResolveCommit("https://github.com/o/r.git", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", commit)
}

func TestDiscoveryVersionForEnforcementGit(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" {
			return 0, "cafebabecafebabecafebabecafebabecafebabe\trefs/tags/v2.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	got, err := discoveryVersionForEnforcement("github:owner/repo", "v2.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0+cafebabecafebabecafebabecafebabecafebabe", got)

	got, err = discoveryVersionForEnforcement("npm:eslint", "9.0.0")
	require.NoError(t, err)
	assert.Equal(t, "9.0.0", got)
}

func TestEnrichDiscoveryPairGit(t *testing.T) {
	enriched, err := enrichDiscoveryPair(DiscoveryPair{
		SourceID: "github:o/r",
		Version:  "v1.0.0",
		Commit:   "1111111111111111111111111111111111111111",
	})
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0+1111111111111111111111111111111111111111", enriched.Version)

	_, err = enrichDiscoveryPair(DiscoveryPair{SourceID: "github:o/r", Version: "v1.0.0"})
	require.Error(t, err)

	enriched, err = enrichDiscoveryPair(DiscoveryPair{SourceID: "pypi:black", Version: "1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", enriched.Version)
}

func TestGitRepoURLFromSourceID(t *testing.T) {
	url, err := gitRepoURLFromSourceID("github:owner/repo")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/owner/repo.git", url)

	url, err = gitRepoURLFromSourceID("gitlab:group/project")
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/group/project.git", url)

	url, err = gitRepoURLFromSourceID("codeberg:user/pkg")
	require.NoError(t, err)
	assert.Equal(t, "https://codeberg.org/user/pkg.git", url)

	_, err = gitRepoURLFromSourceID("npm:eslint")
	require.Error(t, err)
}

func TestParseLsRemoteDefaultBranch(t *testing.T) {
	assert.Equal(t, "release", parseLsRemoteDefaultBranch("ref: refs/heads/release\tHEAD\na12fd5672110c8aa7e3c8419e28c96943ca179be\tHEAD\n"))
	assert.Equal(t, "main", parseLsRemoteDefaultBranch("ref: refs/heads/main\tHEAD\n"))
	assert.Equal(t, "", parseLsRemoteDefaultBranch("a12fd5672110c8aa7e3c8419e28c96943ca179be\tHEAD\n"))
}

func TestResolveGitDefaultBranchFromRemote(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" {
			return 0, "ref: refs/heads/release\tHEAD\n", nil
		}
		return 1, "", nil
	}

	assert.Equal(t, "release", ResolveGitDefaultBranch("https://github.com/github/copilot.vim.git", ""))
}

func TestIsGenericDefaultBranchAlias(t *testing.T) {
	assert.True(t, IsGenericDefaultBranchAlias("main"))
	assert.True(t, IsGenericDefaultBranchAlias("HEAD"))
	assert.False(t, IsGenericDefaultBranchAlias("release"))
}

func TestDiscoverGitRemoteLatestPrefersSemverTag(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	oldDate := fetchGitCommitDateFn
	defer func() { fetchGitCommitDateFn = oldDate }()
	fetchGitCommitDateFn = func(string, string) (time.Time, error) {
		return time.Time{}, assert.AnError
	}
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 2 && args[0] == "ls-remote" && args[1] == "--tags" {
			return 0, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/tags/v1.0.0\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v2.0.0\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/tags/v2.0.0^{}" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v2.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	ver, commit, err := DiscoverGitRemoteLatest("github:o/plugin", "main")
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", ver)
	assert.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", commit)
}

func TestDiscoverGitRemoteLatestPrefersBranchWhenTagStale(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	oldDate := fetchGitCommitDateFn
	defer func() { fetchGitCommitDateFn = oldDate }()
	oldPolicy := GetPreferBranchPolicy()
	defer SetPreferBranchPolicy(oldPolicy)
	SetPreferBranchPolicy(PreferBranchPolicy{
		Branches: []string{"main", "master"},
		Kind:     config.PreferBranchWhenReleaseAgeGap,
		Gap:      60 * 24 * time.Hour,
	})

	now := time.Now()
	fetchGitCommitDateFn = func(_ string, sha string) (time.Time, error) {
		switch sha {
		case "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
			return now.Add(-90 * 24 * time.Hour), nil
		case "dddddddddddddddddddddddddddddddddddddddd":
			return now.Add(-1 * 24 * time.Hour), nil
		default:
			return time.Time{}, assert.AnError
		}
	}
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 2 && args[0] == "ls-remote" && args[1] == "--tags" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v2.0.0\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/tags/v2.0.0^{}" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v2.0.0^{}\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/heads/main" {
			return 0, "dddddddddddddddddddddddddddddddddddddddd\trefs/heads/main\n", nil
		}
		return 1, "", nil
	}

	ver, commit, err := DiscoverGitRemoteLatest("github:o/plugin", "main")
	require.NoError(t, err)
	assert.Equal(t, "main", ver)
	assert.Equal(t, "dddddddddddddddddddddddddddddddddddddddd", commit)
}

func TestDiscoverGitRemoteLatestAlwaysPrefersBranch(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	oldPolicy := GetPreferBranchPolicy()
	defer SetPreferBranchPolicy(oldPolicy)
	SetPreferBranchPolicy(PreferBranchPolicy{
		Branches: []string{"main"},
		Kind:     config.PreferBranchWhenAlways,
	})

	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 2 && args[0] == "ls-remote" && args[1] == "--tags" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v2.0.0\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/tags/v2.0.0^{}" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v2.0.0^{}\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/heads/main" {
			return 0, "dddddddddddddddddddddddddddddddddddddddd\trefs/heads/main\n", nil
		}
		return 1, "", nil
	}

	ver, commit, err := DiscoverGitRemoteLatest("github:o/plugin", "main")
	require.NoError(t, err)
	assert.Equal(t, "main", ver)
	assert.Equal(t, "dddddddddddddddddddddddddddddddddddddddd", commit)
}

func TestDiscoverGitRemoteLatestFallsBackToBranch(t *testing.T) {
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 2 && args[0] == "ls-remote" && args[1] == "--tags" {
			return 0, "", nil // no tags
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" {
			return 0, "ref: refs/heads/main\tHEAD\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" && args[2] == "refs/heads/main" {
			return 0, "dddddddddddddddddddddddddddddddddddddddd\trefs/heads/main\n", nil
		}
		return 1, "", nil
	}

	ver, commit, err := DiscoverGitRemoteLatest("github:o/plugin", "latest")
	require.NoError(t, err)
	assert.Equal(t, "main", ver)
	assert.Equal(t, "dddddddddddddddddddddddddddddddddddddddd", commit)
}

func TestDiscoverNonRegistryGitPackagesFiltersAndRecords(t *testing.T) {
	_ = withTempNvpmHome(t)
	SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { SetDiscoveryWritesEnabled(true) })

	oldFn := discoverGitRemoteLatestFn
	defer func() { discoverGitRemoteLatestFn = oldFn }()
	discoverGitRemoteLatestFn = func(sourceID, installedVersion string) (string, string, error) {
		assert.Equal(t, "github:o/manual", sourceID)
		return "v9.9.9", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", nil
	}

	pkgs := []local_packages_parser.LocalPackageItem{
		{SourceID: "npm:eslint", Version: "8.0.0"},
		{SourceID: "github:o/inreg", Version: "v1.0.0", Commit: "1111111111111111111111111111111111111111"},
		{SourceID: "github:o/manual", Version: "main", Commit: "0000000000000000000000000000000000000000"},
	}
	inRegistry := func(id string) bool {
		return id == "npm:eslint" || id == "github:o/inreg"
	}

	require.NoError(t, DiscoverNonRegistryGitPackages(pkgs, inRegistry, false))

	entry, ok, err := GetRemoteLatest("github:o/manual")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "v9.9.9", entry.Version)
	assert.Equal(t, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", entry.Commit)

	_, ok, err = GetRemoteLatest("github:o/inreg")
	require.NoError(t, err)
	assert.False(t, ok)

	vers, err := ListDiscoveredVersions("github:o/manual")
	require.NoError(t, err)
	require.NotEmpty(t, vers)
	assert.Equal(t, "v9.9.9+eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", vers[0].Version)
}
