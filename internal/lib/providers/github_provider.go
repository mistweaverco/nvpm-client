package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

type GitHubProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
	BASE_URL         string
}

// Injectable shell and OS helpers for tests
var githubShellOut = shell_out.ShellOut
var githubShellOutCapture = shell_out.ShellOutCapture
var githubStat = os.Stat
var githubMkdir = os.Mkdir
var githubMkdirAll = os.MkdirAll
var githubLstat = os.Lstat
var githubRemove = os.Remove
var githubRemoveAll = os.RemoveAll
var githubSymlink = os.Symlink
var githubReadDir = os.ReadDir
var githubHasCommand = shell_out.HasCommand

// Injectable local packages helpers for tests
var lppGithubRemove = local_packages_parser.RemoveLocalPackage
var lppGithubGetDataForProvider = local_packages_parser.GetDataForProvider

// Injectable registry parser for tests
var githubRegistryParser = registry_parser.NewDefaultRegistryParser

// Injectable HTTP client for tests
var githubHTTPGet = http.Get

func NewProviderGitHub() *GitHubProvider {
	p := &GitHubProvider{}
	p.PROVIDER_NAME = "github"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	p.BASE_URL = "https://github.com"
	return p
}

func (p *GitHubProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:github/user/repo) and new (github:user/repo) formats
	normalized := normalizePackageID(sourceID)
	if strings.HasPrefix(normalized, p.PREFIX) {
		return strings.TrimPrefix(normalized, p.PREFIX)
	}
	// Fallback for legacy format
	re := regexp.MustCompile("^pkg:" + p.PROVIDER_NAME + "/(.*)")
	matches := re.FindStringSubmatch(sourceID)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (p *GitHubProvider) getRepoURL(repo string) string {
	return fmt.Sprintf("%s/%s.git", p.BASE_URL, repo)
}

func (p *GitHubProvider) packagesDir(sourceID string) string {
	if IsEditorPluginPackage(sourceID) {
		return filepath.Join(files.GetAppNeovimPluginsPath(), p.PROVIDER_NAME)
	}
	return p.APP_PACKAGES_DIR
}

func (p *GitHubProvider) alternatePackagesDir(sourceID string) string {
	if IsEditorPluginPackage(sourceID) {
		return p.APP_PACKAGES_DIR
	}
	return filepath.Join(files.GetAppNeovimPluginsPath(), p.PROVIDER_NAME)
}

func (p *GitHubProvider) getRepoPath(sourceID, repo string) string {
	// Sanitize repo path for filesystem (replace / with _)
	safeRepo := strings.ReplaceAll(repo, "/", "_")
	return resolveInstalledGitRepoPath(
		p.packagesDir(sourceID),
		p.alternatePackagesDir(sourceID),
		safeRepo,
		githubStat,
	)
}

func (p *GitHubProvider) checkGitAvailable() bool {
	return githubHasCommand("git", []string{"--version"}, nil)
}

func (p *GitHubProvider) Install(sourceID, version string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("GitHub Install: Invalid source ID format")
		return false
	}

	registry := githubRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)

	// Shorthand github:package-name (no owner/repo slash) - resolve to github:owner/repo when the
	// registry has a package whose name (or alias) matches the segment after "github:".
	if !strings.Contains(repo, "/") && registryItem.Source.ID == "" {
		if hit := registry.GetByNameOrAlias(repo); hit.Source.ID != "" {
			norm := normalizePackageID(hit.Source.ID)
			if strings.HasPrefix(norm, "github:") {
				Logger.Info(fmt.Sprintf("GitHub Install: Resolved shorthand %q to %q", sourceID, hit.Source.ID))
				sourceID = hit.Source.ID
				repo = p.getRepo(sourceID)
				registryItem = hit
			}
		}
	}
	if !strings.Contains(repo, "/") {
		Logger.Error(fmt.Sprintf(
			"GitHub Install: repository %q is missing the owner; use github:owner/repo (for example github:tree-sitter/tree-sitter-typescript)",
			sourceID,
		))
		return false
	}

	// If registry has asset information, use release download method
	if len(registryItem.Source.Asset) > 0 {
		if IsEditorPluginPackage(sourceID) {
			Logger.Error("GitHub Install: Neovim plugins cannot be installed from release assets; use a git-based registry entry")
			return false
		}
		return p.installFromRelease(sourceID, repo, version, registryItem)
	}

	// Fallback to git clone method
	return p.installFromGit(sourceID, repo, version)
}

func (p *GitHubProvider) installFromRelease(sourceID, repo, version string, registryItem registry_parser.RegistryItem) bool {
	// Find matching asset for current platform
	asset := FindMatchingAsset(registryItem.Source.Asset)
	if asset == nil {
		Logger.Error("GitHub Install: No matching asset found for current platform")
		return false
	}

	// Resolve version
	resolvedVersion := version
	// Branch-like versions don't map to GitHub Releases. Treat them as "latest" for
	// release-asset installs so we don't fall back to cloning and pollute the bin dir.
	switch resolvedVersion {
	case "main", "master", "trunk":
		resolvedVersion = "latest"
	}

	if resolvedVersion == "" || resolvedVersion == "latest" {
		resolvedVersion = registryItem.Version
		if resolvedVersion == "" {
			// Try to get latest release from GitHub API
			latestTag, err := p.getLatestReleaseTag(repo)
			if err != nil {
				Logger.Error(fmt.Sprintf("GitHub Install: Could not determine latest version: %v", err))
				return false
			}
			resolvedVersion = latestTag
		}
	}

	// Resolve asset filename with template variables
	assetFileName := AssetArchiveFileName(asset.File, resolvedVersion)

	// Download release asset
	releaseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, resolvedVersion, assetFileName)
	Logger.Info(fmt.Sprintf("GitHub Install: Downloading release asset from %s", releaseURL))

	// Ensure packages directory exists (create parent directories if needed)
	if err := githubMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error creating packages directory: %v", err))
		return false
	}

	// Create temporary directory for extraction
	tempDir := filepath.Join(p.APP_PACKAGES_DIR, repo+"_temp")
	if err := githubMkdirAll(tempDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error creating temp directory: %v", err))
		return false
	}
	defer githubRemoveAll(tempDir)

	// Download asset
	assetPath := filepath.Join(tempDir, filepath.Base(assetFileName))
	if err := p.downloadAsset(releaseURL, assetPath); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error downloading asset: %v", err))
		return false
	}

	// Extract asset
	extractDir := filepath.Join(tempDir, "extracted")
	if err := githubMkdirAll(extractDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error creating extract directory: %v", err))
		return false
	}

	if err := p.extractArchive(assetPath, extractDir); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error extracting asset: %v", err))
		return false
	}

	// Find binaries and create symlinks
	repoPath := p.getRepoPath(sourceID, repo)
	if err := githubMkdirAll(repoPath, 0755); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error creating package directory: %v", err))
		return false
	}

	// Copy extracted release contents into the package directory
	if err := InstallReleaseAssetContents(extractDir, repoPath, asset); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error installing release contents: %v", err))
		return false
	}

	// Clean up any legacy symlinks from prior git installs.
	// (Those used relative symlinks into the repo dir, which must be removed before
	// we create the curated bin links from the registry.)
	_ = p.removeSymlinks(sourceID, repo)

	// Create symlinks or exec wrappers
	if err := LinkReleaseBins(repoPath, asset, registryItem); err != nil {
		Logger.Info(fmt.Sprintf("GitHub Install: Warning creating bin links: %v", err))
	}

	// Add to local packages
	repoURL := p.getRepoURL(repo)
	if err := persistGitHostedPackage(sourceID, resolvedVersion, "", repoURL); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error adding package to local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("GitHub Install: Successfully installed %s@%s from release", repo, resolvedVersion))
	return true
}

func (p *GitHubProvider) installFromGit(sourceID, repo, version string) bool {
	if !p.checkGitAvailable() {
		Logger.Error("GitHub Install: git command not found. Please install git.")
		return false
	}

	registry := githubRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)

	repoPath, resolvedVersion, ok := p.gitCloneAndCheckout(sourceID, repo, version)
	if !ok {
		return false
	}

	isPlugin := IsEditorPluginPackage(sourceID)

	// If this is a Tree-sitter parser package, build artifacts and run requested integrations.
	if !isPlugin {
		pins, err := buildAndMaybeIntegrateTreeSitter(repoPath, registryItem, resolvedVersion, nil)
		if err != nil {
			Logger.Error(fmt.Sprintf("GitHub Install: Error building tree-sitter parsers: %v", err))
			return false
		}
		if len(pins) > 0 {
			if err := local_packages_parser.MergePackageTreeSitterExternalQueryPins(sourceID, pins); err != nil {
				Logger.Info(fmt.Sprintf("GitHub Install: Warning persisting external query pins: %v", err))
			}
		}
	}

	// Add to local packages
	repoURL := p.getRepoURL(repo)
	if err := persistGitHostedPackage(sourceID, resolvedVersion, repoPath, repoURL); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Install: Error adding package to local packages: %v", err))
		return false
	}
	if isPlugin {
		if err := local_packages_parser.MergePackageKind(sourceID, KindNeovimPlugin); err != nil {
			Logger.Info(fmt.Sprintf("GitHub Install: Warning persisting plugin kind: %v", err))
		}
	}

	// Create symlinks for binaries (tool packages only)
	if !isPlugin {
		if err := p.createSymlinks(repo, repoPath); err != nil {
			Logger.Info(fmt.Sprintf("GitHub Install: Warning creating symlinks: %v", err))
		}
	}

	Logger.Info(fmt.Sprintf("GitHub Install: Successfully installed %s@%s", repo, resolvedVersion))
	return true
}

func (p *GitHubProvider) Remove(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("GitHub Remove: Invalid source ID format")
		return false
	}

	// Remove Neovim tree-sitter parser(s) if this package installed them.
	registry := githubRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)
	if err := removeNeovimTreeSitterParsers(registryItem); err != nil {
		Logger.Info(fmt.Sprintf("GitHub Remove: Warning removing Neovim tree-sitter parsers: %v", err))
	}

	repoPath := p.getRepoPath(sourceID, repo)
	Logger.Info(fmt.Sprintf("GitHub Remove: Removing package %s", repo))

	// Remove symlinks (tool packages only)
	if !IsEditorPluginPackage(sourceID) {
		if err := p.removeSymlinks(sourceID, repo); err != nil {
			Logger.Info(fmt.Sprintf("GitHub Remove: Warning removing symlinks: %v", err))
		}
	}

	// Remove repository directory
	if _, err := githubStat(repoPath); err == nil {
		if err := githubRemoveAll(repoPath); err != nil {
			Logger.Error(fmt.Sprintf("GitHub Remove: Error removing repository directory: %v", err))
			return false
		}
	}

	// Remove from local packages
	if err := lppGithubRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("GitHub Remove: Error removing package from local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("GitHub Remove: Successfully removed %s", repo))
	return true
}

func (p *GitHubProvider) Update(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("GitHub Update: Invalid source ID format")
		return false
	}

	repoPath := p.getRepoPath(sourceID, repo)
	if _, err := githubStat(repoPath); os.IsNotExist(err) {
		Logger.Error(fmt.Sprintf("GitHub Update: Repository %s is not installed", repo))
		return false
	}

	// Fetch latest changes
	code, err := githubShellOut("git", []string{"fetch", "--tags", "origin"}, repoPath, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("GitHub Update: Error fetching updates: %v", err))
		return false
	}

	// Get latest version
	latestVersion, err := p.getLatestVersionFromRepo(repoPath)
	if err != nil {
		// No tags found, use default branch
		latestVersion = p.getDefaultBranch(repo, repoPath)
	}

	Logger.Info(fmt.Sprintf("GitHub Update: Updating %s to version %s", repo, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *GitHubProvider) getLatestVersion(repo string) (string, error) {
	// Prefer the latest GitHub release tag (works for binary release installs).
	// If a repo doesn't publish releases, fall back to the default branch.
	if tag, err := p.getLatestReleaseTag(repo); err == nil && strings.TrimSpace(tag) != "" {
		return strings.TrimSpace(tag), nil
	}
	return p.getDefaultBranch(repo, ""), nil
}

func (p *GitHubProvider) getLatestVersionFromRepo(repoPath string) (string, error) {
	// Fetch tags first
	githubShellOut("git", []string{"fetch", "--tags", "origin"}, repoPath, nil)

	// Get latest tag
	code, output, err := githubShellOutCapture("git", []string{"describe", "--tags", "--abbrev=0"}, repoPath, nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("no tags found")
	}

	tag := strings.TrimSpace(output)
	if tag == "" {
		return "", fmt.Errorf("no tags found")
	}

	return tag, nil
}

func (p *GitHubProvider) getDefaultBranch(repo, repoPath string) string {
	return ResolveGitDefaultBranch(p.getRepoURL(repo), repoPath)
}

func (p *GitHubProvider) createSymlinks(_ string, repoPath string) error {
	nvpmBinDir := files.GetAppBinPath()

	// Look for common binary locations
	binDirs := []string{
		filepath.Join(repoPath, "bin"),
		filepath.Join(repoPath, "target", "release"),
		filepath.Join(repoPath, "dist"),
	}

	for _, binDir := range binDirs {
		if entries, err := githubReadDir(binDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				// Only symlink executables to avoid polluting the bin dir.
				binPath := filepath.Join(binDir, entry.Name())
				if info, err := githubStat(binPath); err == nil {
					if info.Mode()&0111 == 0 {
						continue
					}
					// Skip hidden files and common non-binary files
					if strings.HasPrefix(entry.Name(), ".") {
						continue
					}
					// Create symlink
					symlink := filepath.Join(nvpmBinDir, entry.Name())
					// Remove existing symlink if it exists
					if _, err := githubLstat(symlink); err == nil {
						githubRemove(symlink)
					}
					// Create relative symlink
					relPath, err := filepath.Rel(nvpmBinDir, binPath)
					if err != nil {
						relPath = binPath
					}
					if err := githubSymlink(relPath, symlink); err != nil {
						Logger.Info(fmt.Sprintf("GitHub: Warning creating symlink %s -> %s: %v", symlink, relPath, err))
					} else {
						Logger.Info(fmt.Sprintf("GitHub: Created symlink %s -> %s", symlink, relPath))
					}
					// Only process first executable found per directory to avoid clutter
					break
				}
			}
		}
	}

	return nil
}

func (p *GitHubProvider) removeSymlinks(sourceID, repo string) error {
	repoPath := p.getRepoPath(sourceID, repo)
	repoPath = filepath.Clean(repoPath) + string(os.PathSeparator)
	nvpmBinDir := files.GetAppBinPath()

	// Find and remove symlinks that point to this repo
	entries, err := githubReadDir(nvpmBinDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		symlink := filepath.Join(nvpmBinDir, entry.Name())
		if link, err := githubLstat(symlink); err == nil {
			// Check if it's a symlink
			if link.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(symlink)
				if err != nil {
					continue
				}
				// Resolve relative path
				if !filepath.IsAbs(target) {
					target = filepath.Join(nvpmBinDir, target)
				}
				target = filepath.Clean(target) + string(os.PathSeparator)
				// Check if target is in our repo path
				if strings.HasPrefix(target, repoPath) {
					if err := githubRemove(symlink); err != nil {
						Logger.Info(fmt.Sprintf("GitHub: Warning removing symlink %s: %v", symlink, err))
					}
				}
			}
		}
	}

	return nil
}

func (p *GitHubProvider) Sync() bool {
	Logger.Info("GitHub Sync: Syncing GitHub packages")
	localPackages := lppGithubGetDataForProvider(p.PROVIDER_NAME).Packages

	allOk := true
	for _, pkg := range localPackages {
		repo := p.getRepo(pkg.SourceID)
		if repo == "" {
			continue
		}
		repoPath := p.getRepoPath(pkg.SourceID, repo)
		if _, err := githubStat(repoPath); os.IsNotExist(err) {
			// Re-install missing packages
			Logger.Info(fmt.Sprintf("GitHub Sync: Re-installing missing package %s", repo))
			if !p.Install(pkg.SourceID, pkg.Version) {
				allOk = false
			}
		} else if !IsEditorPluginPackage(pkg.SourceID) {
			// Update symlinks for tool packages
			if err := p.createSymlinks(repo, repoPath); err != nil {
				Logger.Info(fmt.Sprintf("GitHub Sync: Warning creating symlinks for %s: %v", repo, err))
			}
		}
	}

	return allOk
}

// getLatestReleaseTag gets the latest release tag from GitHub API
func (p *GitHubProvider) getLatestReleaseTag(repo string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	resp, err := githubHTTPGet(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	return release.TagName, nil
}

// downloadAsset downloads a file from a URL to a destination path
func (p *GitHubProvider) downloadAsset(url, destPath string) error {
	resp, err := githubHTTPGet(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// extractArchive extracts an archive (tar.gz, zip, etc.) to a destination directory
func (p *GitHubProvider) extractArchive(archivePath, destDir string) error {
	ext := filepath.Ext(archivePath)
	baseExt := filepath.Ext(strings.TrimSuffix(archivePath, ext))

	if baseExt == ".tar" && ext == ".gz" {
		// Extract tar.gz
		code, err := githubShellOut("tar", []string{"-xzf", archivePath, "-C", destDir}, "", nil)
		if err != nil || code != 0 {
			return fmt.Errorf("failed to extract tar.gz: %v", err)
		}
		return nil
	} else if ext == ".zip" {
		// Use files.Unzip
		if err := files.Unzip(archivePath, destDir); err != nil {
			return fmt.Errorf("failed to extract zip: %w", err)
		}
		return nil
	} else if ext == ".gz" && baseExt != ".tar" {
		// Single .gz file - gunzip and copy
		outputPath := filepath.Join(destDir, strings.TrimSuffix(filepath.Base(archivePath), ".gz"))
		code, err := githubShellOut("sh", []string{"-c", fmt.Sprintf("gunzip -c %s > %s", archivePath, outputPath)}, "", nil)
		if err != nil || code != 0 {
			return fmt.Errorf("failed to extract gz: %v", err)
		}
		return nil
	}

	// If no extension or unknown format, assume it's a single binary file
	// Just copy it
	srcFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	destPath := filepath.Join(destDir, filepath.Base(archivePath))
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}
