package providers

import (
	"fmt"
	"os"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

// GitHubTreeSitterUsesPhasedInteractiveInstall is true when this install should split into
// (1) interactive prefights without a spinner and (2) build/integrate behind the CLI spinner.
func GitHubTreeSitterUsesPhasedInteractiveInstall(sourceID string, registryItem registry_parser.RegistryItem) bool {
	if registryItem.Source.ID == "" || !IsTreeSitterCategory(registryItem.Categories) {
		return false
	}
	if registryItem.TreeSitter == nil || len(registryItem.TreeSitter.Build) == 0 {
		return false
	}
	if !registryDeclaresNeovimTreeSitterIntegration(registryItem) {
		return false
	}
	if !integrationEnabled("neovim") {
		return false
	}
	id := strings.ToLower(strings.TrimSpace(sourceID))
	return strings.HasPrefix(id, "github:")
}

type githubTreeSitterDeferredState struct {
	p                      *GitHubProvider
	sourceID               string
	repo                   string
	repoPath               string
	resolvedVersion        string
	registryItem           registry_parser.RegistryItem
	externalQueryPreflight *ExternalQueryPreflightChoice // nil: cache step runs lock-aware confirm; non-nil: preflight already asked
	builtLangs             []string
	externalQueryPins      []local_packages_parser.TreeSitterExternalQueryPin
}

var githubTreeSitterDeferred *githubTreeSitterDeferredState

func clearGitHubTreeSitterDeferred() {
	githubTreeSitterDeferred = nil
}

// ClearGitHubTreeSitterDeferred drops deferred phased-install state (e.g. after a cancelled step).
func ClearGitHubTreeSitterDeferred() {
	clearGitHubTreeSitterDeferred()
}

func githubTreeSitterDeferredGet(sourceID, resolvedVersion string) (*githubTreeSitterDeferredState, bool) {
	d := githubTreeSitterDeferred
	if d == nil || d.sourceID != sourceID || d.resolvedVersion != resolvedVersion {
		return nil, false
	}
	return d, true
}

func getGitHubProviderConcrete() (*GitHubProvider, bool) {
	pm := getGitHubProvider()
	p, ok := pm.(*GitHubProvider)
	return p, ok
}

// GitHubTreeSitterPreflightInteractive clones/checks out the grammar repo, resolves Neovim inherit
// dependencies, and runs the external tree-sitter query confirmation when needed. Call this
// before the install spinners; then run GitHubTreeSitterPhaseBuildParsers, PhaseCacheNeovimQueries
// (external query clones show their own spinners), and PhaseRegisterPackage as wired from the CLI.
func GitHubTreeSitterPreflightInteractive(sourceID, resolvedVersion string) error {
	reg := githubRegistryParser().GetBySourceId(sourceID)
	if !GitHubTreeSitterUsesPhasedInteractiveInstall(sourceID, reg) {
		return fmt.Errorf("internal: GitHubTreeSitterPreflightInteractive called for non-phased package")
	}
	if reg.Source.ID == "" {
		return fmt.Errorf("unknown registry package %q", sourceID)
	}
	clearGitHubTreeSitterDeferred()

	if err := ensureTreeSitterParserRequirements(reg, resolvedVersion); err != nil {
		return err
	}

	p, ok := getGitHubProviderConcrete()
	if !ok {
		return fmt.Errorf("internal: GitHub provider unavailable")
	}
	repo := p.getRepo(sourceID)
	if repo == "" {
		return fmt.Errorf("invalid GitHub source id %q", sourceID)
	}

	repoPath, ver, ok := p.gitCloneAndCheckout(sourceID, repo, resolvedVersion)
	if !ok {
		return fmt.Errorf("git checkout failed for %s", sourceID)
	}

	if err := ensureNeovimTreeSitterInheritDependencies(reg); err != nil {
		clearGitHubTreeSitterDeferred()
		return err
	}
	if err := ensureNeovimTreeSitterInjectionQueryPackages(reg, ver); err != nil {
		clearGitHubTreeSitterDeferred()
		return err
	}

	planned := plannedTreeSitterBuildLanguages(reg.TreeSitter.Build)
	needs, err := collectExternalTreeSitterQueryNeeds(repoPath, reg.TreeSitter.Build, planned)
	if err != nil {
		clearGitHubTreeSitterDeferred()
		return err
	}
	needsConfirm := externalQueryNeedsStillRequiringConfirm(sourceID, ver, needs)
	var pre *ExternalQueryPreflightChoice
	if len(needsConfirm) > 0 {
		allow, err := batchConfirmExternalTreeSitterQueries(sourceID, needsConfirm)
		if err != nil {
			clearGitHubTreeSitterDeferred()
			return err
		}
		pre = &ExternalQueryPreflightChoice{AllowUnpinned: allow}
	} else if len(needs) > 0 {
		// All external-query languages are already pinned in the lockfile at this version.
		pre = &ExternalQueryPreflightChoice{AllowUnpinned: false}
	}

	githubTreeSitterDeferred = &githubTreeSitterDeferredState{
		p:                      p,
		sourceID:               sourceID,
		repo:                   repo,
		repoPath:               repoPath,
		resolvedVersion:        ver,
		registryItem:           reg,
		externalQueryPreflight: pre,
	}
	return nil
}

// GitHubTreeSitterPhaseBuildParsers runs the tree-sitter compile step for a phased install.
// Deferred state must match sourceID and resolvedVersion. On failure, deferred state is cleared.
func GitHubTreeSitterPhaseBuildParsers(sourceID, resolvedVersion string) bool {
	d, ok := githubTreeSitterDeferredGet(sourceID, resolvedVersion)
	if !ok {
		Logger.Error("GitHub tree-sitter phased install: missing or stale deferred state")
		return false
	}
	langs, err := BuildTreeSitterParsersToCache(d.repoPath, d.sourceID, d.resolvedVersion, d.registryItem.TreeSitter.Build)
	if err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error building tree-sitter parsers: %v", err))
		clearGitHubTreeSitterDeferred()
		return false
	}
	d.builtLangs = langs
	return true
}

// GitHubTreeSitterPhaseCacheNeovimQueries caches Neovim query files (including optional external
// git clones, each with its own spinner when stderr is a TTY). Call after a successful
// GitHubTreeSitterPhaseBuildParsers. On failure, deferred state is cleared.
func GitHubTreeSitterPhaseCacheNeovimQueries(sourceID, resolvedVersion string) bool {
	d, ok := githubTreeSitterDeferredGet(sourceID, resolvedVersion)
	if !ok {
		Logger.Error("GitHub tree-sitter phased install: missing or stale deferred state")
		return false
	}
	pins, err := cacheNeovimTreeSitterQueriesForBuiltLangs(
		d.repoPath,
		d.sourceID,
		d.resolvedVersion,
		d.registryItem.TreeSitter.Build,
		d.builtLangs,
		d.externalQueryPreflight,
	)
	if err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error caching Neovim tree-sitter queries: %v", err))
		clearGitHubTreeSitterDeferred()
		return false
	}
	d.externalQueryPins = pins
	return true
}

// GitHubTreeSitterPhaseRegisterPackage installs Neovim parsers from cache, registers the package,
// and creates symlinks. Call after GitHubTreeSitterPhaseCacheNeovimQueries. Always clears deferred state.
func GitHubTreeSitterPhaseRegisterPackage(sourceID, resolvedVersion string) bool {
	d, ok := githubTreeSitterDeferredGet(sourceID, resolvedVersion)
	if !ok {
		Logger.Error("GitHub tree-sitter phased install: missing or stale deferred state")
		return false
	}
	defer clearGitHubTreeSitterDeferred()

	queryOnly := queryOnlyNeovimLanguagesForInstall(d.registryItem.TreeSitter.Build, d.builtLangs)
	if err := installNeovimParsersAndQueriesFromCache(d.sourceID, d.resolvedVersion, d.builtLangs, queryOnly); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error installing Neovim parsers: %v", err))
		return false
	}

	repoURL := d.p.getRepoURL(d.repo)
	if err := persistGitHostedPackage(sourceID, d.resolvedVersion, d.repoPath, repoURL); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error adding package to local packages: %v", err))
		return false
	}
	if len(d.externalQueryPins) > 0 {
		if err := local_packages_parser.MergePackageTreeSitterExternalQueryPins(d.sourceID, d.externalQueryPins); err != nil {
			Logger.Info(fmt.Sprintf("GitHub Install: Warning persisting external query pins: %v", err))
		}
	}

	if err := d.p.createSymlinks(d.repo, d.repoPath); err != nil {
		Logger.Info(fmt.Sprintf("GitHub Install: Warning creating symlinks: %v", err))
	}

	Logger.Info(fmt.Sprintf("GitHub Install: Successfully installed %s@%s", d.repo, d.resolvedVersion))
	return true
}

// GitHubTreeSitterCompleteInteractiveInstall finishes a phased GitHub tree-sitter install in one
// call (no spinner between sub-steps). Prefer GitHubTreeSitterPhase* from the CLI when you need
// spinners during external query clones.
func GitHubTreeSitterCompleteInteractiveInstall(sourceID, resolvedVersion string) bool {
	if !GitHubTreeSitterPhaseBuildParsers(sourceID, resolvedVersion) {
		return false
	}
	if !GitHubTreeSitterPhaseCacheNeovimQueries(sourceID, resolvedVersion) {
		return false
	}
	return GitHubTreeSitterPhaseRegisterPackage(sourceID, resolvedVersion)
}

// gitCloneAndCheckout mirrors the first phase of installFromGit: ensure clone, resolve version, checkout.
func (p *GitHubProvider) gitCloneAndCheckout(sourceID, repo, version string) (repoPath string, resolvedVersion string, ok bool) {
	repoPath = p.getRepoPath(repo)
	repoURL := p.getRepoURL(repo)

	if err := githubMkdir(p.APP_PACKAGES_DIR, 0755); err != nil && !os.IsExist(err) {
		Logger.Error(fmt.Sprintf("GitHub Install: Error creating packages directory: %v", err))
		return "", "", false
	}

	if _, err := githubStat(repoPath); os.IsNotExist(err) {
		Logger.Info(fmt.Sprintf("GitHub Install: Cloning %s to %s", repoURL, repoPath))
		code, err := githubShellOut("git", []string{"clone", repoURL, repoPath}, p.APP_PACKAGES_DIR, nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("GitHub Install: Error cloning %s: %v", repoURL, err))
			return "", "", false
		}
	} else {
		Logger.Info(fmt.Sprintf("GitHub Install: Updating repository at %s", repoPath))
		code, err := githubShellOut("git", []string{"fetch", "origin"}, repoPath, nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("GitHub Install: Error fetching updates: %v", err))
			return "", "", false
		}
	}

	resolvedVersion = version
	if resolvedVersion == "" || resolvedVersion == "latest" {
		var err error
		resolvedVersion, err = p.getLatestVersionFromRepo(repoPath)
		if err != nil {
			Logger.Info(fmt.Sprintf("GitHub Install: Could not determine latest version, using default branch: %v", err))
			resolvedVersion = p.getDefaultBranch(repoPath)
		}
	}

	code, err := githubShellOut("git", []string{"checkout", resolvedVersion}, repoPath, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("GitHub Install: Error checking out version %s: %v", resolvedVersion, err))
		return "", "", false
	}

	return repoPath, resolvedVersion, true
}
