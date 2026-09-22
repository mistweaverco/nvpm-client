package providers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLockSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func stubGitRevParseHEAD(t *testing.T, sha string, err error) {
	t.Helper()
	old := gitRevParseHEADFn
	t.Cleanup(func() { gitRevParseHEADFn = old })
	gitRevParseHEADFn = func(string) (string, error) { return sha, err }
}

func TestInstalledMatchesLockGitHEAD(t *testing.T) {
	_ = withTempNvpmHome(t)
	t.Cleanup(ResetLockedCommit)

	sourceID := "github:tree-sitter/tree-sitter-python"
	p := NewProviderGitHub()
	repoPath := p.getRepoPath(sourceID, p.getRepo(sourceID))
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755))
	stubGitRevParseHEAD(t, testLockSHA, nil)

	pkg := local_packages_parser.LocalPackageItem{
		SourceID: sourceID,
		Version:  "master",
		Commit:   testLockSHA,
	}
	assert.True(t, InstalledMatchesLock(pkg))

	pkg.Commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	assert.False(t, InstalledMatchesLock(pkg))

	pkg.Commit = ""
	assert.False(t, InstalledMatchesLock(pkg))
}

func TestInstalledMatchesLockGitMissingWorktree(t *testing.T) {
	_ = withTempNvpmHome(t)
	pkg := local_packages_parser.LocalPackageItem{
		SourceID: "github:owner/missing",
		Version:  "main",
		Commit:   testLockSHA,
	}
	assert.False(t, InstalledMatchesLock(pkg))
}

func TestResolveNestedDependencyInstallSpecPrefersLock(t *testing.T) {
	_ = withTempNvpmHome(t)
	dep := "github:tree-sitter/tree-sitter-python"
	require.NoError(t, local_packages_parser.AddLocalPackageWithCommit(dep, "0.23.0", testLockSHA))

	ver, commit := resolveNestedDependencyInstallSpec(dep)
	assert.Equal(t, "0.23.0", ver)
	assert.Equal(t, testLockSHA, commit)
}

func TestGitHEADMatchesCommit(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, gitHEADMatchesCommit(dir, testLockSHA))

	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	stubGitRevParseHEAD(t, testLockSHA, nil)
	assert.True(t, gitHEADMatchesCommit(dir, testLockSHA))
	assert.True(t, gitHEADMatchesCommit(dir, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	assert.False(t, gitHEADMatchesCommit(dir, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
}

func TestGitCloneAndCheckoutSkipsFetchWhenHEADMatchesLock(t *testing.T) {
	_ = withTempNvpmHome(t)
	t.Cleanup(ResetLockedCommit)

	sourceID := "github:tree-sitter/tree-sitter-python"
	p := NewProviderGitHub()
	repo := p.getRepo(sourceID)
	repoPath := p.getRepoPath(sourceID, repo)
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755))

	SetLockedCommit(sourceID, testLockSHA)
	stubGitRevParseHEAD(t, testLockSHA, nil)

	oldOut := githubShellOut
	oldCap := githubShellOutCapture
	t.Cleanup(func() {
		githubShellOut = oldOut
		githubShellOutCapture = oldCap
	})
	githubShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("git clone/checkout should be skipped when HEAD matches lock")
		return 1, errors.New("unexpected git")
	}
	githubShellOutCapture = func(string, []string, string, []string) (int, string, error) {
		t.Fatal("git fetch should be skipped when HEAD matches lock")
		return 1, "", errors.New("unexpected git")
	}

	gotPath, ver, ok := p.gitCloneAndCheckout(sourceID, repo, "master")
	require.True(t, ok)
	assert.Equal(t, repoPath, gotPath)
	assert.Equal(t, "master", ver)
}

func TestGitHubSyncSkipsInstallWhenHEADMatchesLock(t *testing.T) {
	_ = withTempNvpmHome(t)
	t.Cleanup(ResetLockedCommit)

	sourceID := "github:owner/tool"
	p := NewProviderGitHub()
	repo := p.getRepo(sourceID)
	repoPath := p.getRepoPath(sourceID, repo)
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755))
	stubGitRevParseHEAD(t, testLockSHA, nil)

	oldGet := lppGithubGetDataForProvider
	t.Cleanup(func() { lppGithubGetDataForProvider = oldGet })
	lppGithubGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{
			Packages: []local_packages_parser.LocalPackageItem{{
				SourceID: sourceID,
				Version:  "main",
				Commit:   testLockSHA,
			}},
		}
	}

	oldOut := githubShellOut
	oldCap := githubShellOutCapture
	t.Cleanup(func() {
		githubShellOut = oldOut
		githubShellOutCapture = oldCap
	})
	githubShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("GitHub Sync should not install when HEAD already matches lock")
		return 1, errors.New("unexpected git")
	}
	githubShellOutCapture = func(string, []string, string, []string) (int, string, error) {
		t.Fatal("GitHub Sync should not fetch when HEAD already matches lock")
		return 1, "", errors.New("unexpected git")
	}

	assert.True(t, p.Sync())
}

func TestEnforceGitTagSHAOrRejectLockedCommitIsSourceScoped(t *testing.T) {
	_ = withTempNvpmHome(t)
	t.Cleanup(ResetLockedCommit)
	SetDiscoveryWritesEnabled(true)
	ClearLastError()

	parent := "github:nvim-treesitter/nvim-treesitter"
	dep := "github:mistweaverco/floaterm.nvim"
	oldCommit := "19198f485082474248b5919f6aa0e473a2dd9726"
	newCommit := "301ea764263d0c1a42a8fc2985047c0012347401"
	require.NoError(t, RecordDiscovery(dep, FormatGitDiscoveryVersion("v1.1.0", oldCommit)))

	oldCap := gitDiscoveryShellOutCapture
	t.Cleanup(func() { gitDiscoveryShellOutCapture = oldCap })
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 1 && args[0] == "ls-remote" {
			return 0, newCommit + "\trefs/tags/v1.1.0\n", nil
		}
		return 1, "", nil
	}

	SetLockedCommit(parent, testLockSHA)
	assert.False(t, enforceGitTagSHAOrReject(dep, "v1.1.0"), "nested dep must not inherit parent lock SHA bypass")
	assert.True(t, enforceGitTagSHAOrReject(parent, "main"))
}
