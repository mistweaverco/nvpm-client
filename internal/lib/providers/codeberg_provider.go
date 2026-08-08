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

type CodebergProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
	BASE_URL         string
}

// Injectable shell and OS helpers for tests
var codebergShellOut = shell_out.ShellOut
var codebergShellOutCapture = shell_out.ShellOutCapture
var codebergStat = os.Stat
var codebergMkdirAll = os.MkdirAll
var codebergLstat = os.Lstat
var codebergRemove = os.Remove
var codebergRemoveAll = os.RemoveAll
var codebergSymlink = os.Symlink
var codebergReadDir = os.ReadDir
var codebergHasCommand = shell_out.HasCommand

// Injectable local packages helpers for tests
var lppCodebergRemove = local_packages_parser.RemoveLocalPackage
var lppCodebergGetDataForProvider = local_packages_parser.GetDataForProvider

// Injectable registry parser for tests
var codebergRegistryParser = registry_parser.NewDefaultRegistryParser

// Injectable HTTP client for tests
var codebergHTTPGet = http.Get

func NewProviderCodeberg() *CodebergProvider {
	p := &CodebergProvider{}
	p.PROVIDER_NAME = "codeberg"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	p.BASE_URL = "https://codeberg.org"
	return p
}

func (p *CodebergProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:codeberg/user/repo) and new (codeberg:user/repo) formats
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

func (p *CodebergProvider) getRepoURL(repo string) string {
	return fmt.Sprintf("%s/%s.git", p.BASE_URL, repo)
}

func (p *CodebergProvider) packagesDir(sourceID string) string {
	if IsEditorPluginPackage(sourceID) {
		return filepath.Join(files.GetAppNeovimPluginsPath(), p.PROVIDER_NAME)
	}
	return p.APP_PACKAGES_DIR
}

func (p *CodebergProvider) alternatePackagesDir(sourceID string) string {
	if IsEditorPluginPackage(sourceID) {
		return p.APP_PACKAGES_DIR
	}
	return filepath.Join(files.GetAppNeovimPluginsPath(), p.PROVIDER_NAME)
}

func (p *CodebergProvider) getRepoPath(sourceID, repo string) string {
	// Sanitize repo path for filesystem (replace / with _)
	safeRepo := strings.ReplaceAll(repo, "/", "_")
	return resolveInstalledGitRepoPath(
		p.packagesDir(sourceID),
		p.alternatePackagesDir(sourceID),
		safeRepo,
		codebergStat,
	)
}

func (p *CodebergProvider) checkGitAvailable() bool {
	return codebergHasCommand("git", []string{"--version"}, nil)
}

func (p *CodebergProvider) Install(sourceID, version string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("Codeberg Install: Invalid source ID format")
		return false
	}

	// Check registry for asset information
	registry := codebergRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)

	// If registry has asset information, use release download method
	if len(registryItem.Source.Asset) > 0 {
		if IsEditorPluginPackage(sourceID) {
			Logger.Error("Codeberg Install: Neovim plugins cannot be installed from release assets; use a git-based registry entry")
			return false
		}
		return p.installFromRelease(sourceID, repo, version, registryItem)
	}

	// Fallback to git clone method
	return p.installFromGit(sourceID, repo, version)
}

func (p *CodebergProvider) installFromRelease(sourceID, repo, version string, registryItem registry_parser.RegistryItem) bool {
	// Find matching asset for current platform
	asset := FindMatchingAsset(registryItem.Source.Asset)
	if asset == nil {
		Logger.Error("Codeberg Install: No matching asset found for current platform")
		return false
	}

	// Resolve version
	resolvedVersion := version
	if resolvedVersion == "" || resolvedVersion == "latest" {
		resolvedVersion = registryItem.Version
		if resolvedVersion == "" {
			// Try to get latest release from Codeberg API
			latestTag, err := p.getLatestReleaseTag(repo)
			if err != nil {
				Logger.Error(fmt.Sprintf("Codeberg Install: Could not determine latest version: %v", err))
				return false
			}
			resolvedVersion = latestTag
		}
	}

	// Resolve asset filename with template variables
	assetFileName := AssetArchiveFileName(asset.File, resolvedVersion)

	// Download release asset
	// Codeberg (Gitea) release download URL format: https://codeberg.org/{owner}/{repo}/releases/download/{tag}/{filename}
	releaseURL := fmt.Sprintf("https://codeberg.org/%s/releases/download/%s/%s", repo, resolvedVersion, assetFileName)
	Logger.Info(fmt.Sprintf("Codeberg Install: Downloading release asset from %s", releaseURL))

	// Ensure packages directory exists (create parent directories if needed)
	if err := codebergMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error creating packages directory: %v", err))
		return false
	}

	// Create temporary directory for extraction
	tempDir := filepath.Join(p.APP_PACKAGES_DIR, strings.ReplaceAll(repo, "/", "_")+"_temp")
	if err := codebergMkdirAll(tempDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error creating temp directory: %v", err))
		return false
	}
	defer codebergRemoveAll(tempDir)

	// Download asset
	assetPath := filepath.Join(tempDir, filepath.Base(assetFileName))
	if err := p.downloadAsset(releaseURL, assetPath); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error downloading asset: %v", err))
		return false
	}

	// Extract asset
	extractDir := filepath.Join(tempDir, "extracted")
	if err := codebergMkdirAll(extractDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error creating extract directory: %v", err))
		return false
	}

	if err := p.extractArchive(assetPath, extractDir); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error extracting asset: %v", err))
		return false
	}

	// Find binaries and create symlinks
	repoPath := p.getRepoPath(sourceID, repo)
	if err := codebergMkdirAll(repoPath, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error creating package directory: %v", err))
		return false
	}

	// Copy extracted release contents into the package directory
	if err := InstallReleaseAssetContents(extractDir, repoPath, asset); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error installing release contents: %v", err))
		return false
	}

	// Create symlinks or exec wrappers
	if err := LinkReleaseBins(repoPath, asset, registryItem); err != nil {
		Logger.Info(fmt.Sprintf("Codeberg Install: Warning creating bin links: %v", err))
	}

	// Add to local packages
	repoURL := p.getRepoURL(repo)
	if err := persistGitHostedPackage(sourceID, resolvedVersion, "", repoURL); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error adding package to local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("Codeberg Install: Successfully installed %s@%s from release", repo, resolvedVersion))
	return true
}

func (p *CodebergProvider) installFromGit(sourceID, repo, version string) bool {
	if !p.checkGitAvailable() {
		Logger.Error("Codeberg Install: git command not found. Please install git.")
		return false
	}

	repoPath := p.getRepoPath(sourceID, repo)
	repoURL := p.getRepoURL(repo)
	packagesDir := p.packagesDir(sourceID)

	if err := codebergMkdirAll(packagesDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error creating packages directory: %v", err))
		return false
	}

	isPlugin := IsEditorPluginPackage(sourceID)

	if _, err := codebergStat(repoPath); os.IsNotExist(err) {
		Logger.Info(fmt.Sprintf("Codeberg Install: Cloning %s to %s", repoURL, repoPath))
		code, err := codebergShellOut("git", []string{"clone", repoURL, repoPath}, packagesDir, nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("Codeberg Install: Error cloning repository: %v", err))
			return false
		}
	} else {
		// Update existing repository
		Logger.Info(fmt.Sprintf("Codeberg Install: Updating repository at %s", repoPath))
		code, err := codebergShellOut("git", []string{"fetch", "origin"}, repoPath, nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("Codeberg Install: Error fetching updates: %v", err))
			return false
		}
	}

	// Resolve version label (tag/branch); lockfile commit overrides checkout target below.
	resolvedVersion := version
	lockedCheckout := strings.TrimSpace(GetLockedCommit()) != ""
	if !lockedCheckout && (resolvedVersion == "" || resolvedVersion == "latest") {
		// Try to get latest tag from the cloned repo
		var err error
		resolvedVersion, err = ResolveGitLatestRef(sourceID)
		if err != nil || strings.TrimSpace(resolvedVersion) == "" {
			Logger.Info(fmt.Sprintf("Codeberg Install: Could not determine latest version, using default branch: %v", err))
			resolvedVersion = p.getDefaultBranch(repo, repoPath)
		}
	}

	// Prefer lockfile commit over branch/tag so sync restores the pinned revision.
	versionLabel := resolvedVersion
	checkoutRef := PreferLockedGitCheckoutRef(resolvedVersion)
	checkedOut, checkoutErr := gitCheckoutRefWithBranchFallback(codebergShellOut, repoPath, checkoutRef, p.getDefaultBranch(repo, repoPath))
	if checkoutErr != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error checking out version %s: %v", checkoutRef, checkoutErr))
		return false
	}
	if lockedCheckout {
		resolvedVersion = versionLabel
	} else {
		resolvedVersion = checkedOut
	}

	// Add to local packages
	if err := persistGitHostedPackage(sourceID, resolvedVersion, repoPath, repoURL); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Install: Error adding package to local packages: %v", err))
		return false
	}
	if isPlugin {
		if err := local_packages_parser.MergePackageKind(sourceID, KindNeovimPlugin); err != nil {
			Logger.Info(fmt.Sprintf("Codeberg Install: Warning persisting plugin kind: %v", err))
		}
	}

	if !isPlugin {
		if err := p.createSymlinks(repo, repoPath); err != nil {
			Logger.Info(fmt.Sprintf("Codeberg Install: Warning creating symlinks: %v", err))
		}
	}

	Logger.Info(fmt.Sprintf("Codeberg Install: Successfully installed %s@%s", repo, resolvedVersion))
	return true
}

func (p *CodebergProvider) Remove(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("Codeberg Remove: Invalid source ID format")
		return false
	}

	repoPath := p.getRepoPath(sourceID, repo)
	Logger.Info(fmt.Sprintf("Codeberg Remove: Removing package %s", repo))

	// Remove symlinks
	if !IsEditorPluginPackage(sourceID) {
		if err := p.removeSymlinks(sourceID, repo); err != nil {
			Logger.Info(fmt.Sprintf("Codeberg Remove: Warning removing symlinks: %v", err))
		}
	}

	// Remove repository directory
	if _, err := codebergStat(repoPath); err == nil {
		if err := codebergRemoveAll(repoPath); err != nil {
			Logger.Error(fmt.Sprintf("Codeberg Remove: Error removing repository directory: %v", err))
			return false
		}
	}

	// Remove from local packages
	if err := lppCodebergRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Codeberg Remove: Error removing package from local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("Codeberg Remove: Successfully removed %s", repo))
	return true
}

func (p *CodebergProvider) Update(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("Codeberg Update: Invalid source ID format")
		return false
	}

	repoPath := p.getRepoPath(sourceID, repo)
	if _, err := codebergStat(repoPath); os.IsNotExist(err) {
		Logger.Error(fmt.Sprintf("Codeberg Update: Repository %s is not installed", repo))
		return false
	}

	// Fetch latest changes
	code, err := codebergShellOut("git", []string{"fetch", "--tags", "origin"}, repoPath, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("Codeberg Update: Error fetching updates: %v", err))
		return false
	}

	// Get latest version (prefer-branch-over-release policy)
	latestVersion, err := ResolveGitLatestRef(sourceID)
	if err != nil || strings.TrimSpace(latestVersion) == "" {
		latestVersion = p.getDefaultBranch(repo, repoPath)
	}

	Logger.Info(fmt.Sprintf("Codeberg Update: Updating %s to version %s", repo, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *CodebergProvider) getLatestVersion(repo string) (string, error) {
	return ResolveGitLatestRef("codeberg:" + repo)
}

func (p *CodebergProvider) getLatestVersionFromRepo(repoPath string) (string, error) {
	// Fetch tags first
	codebergShellOut("git", []string{"fetch", "--tags", "origin"}, repoPath, nil)

	// Get latest tag
	code, output, err := codebergShellOutCapture("git", []string{"describe", "--tags", "--abbrev=0"}, repoPath, nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("no tags found")
	}

	tag := strings.TrimSpace(output)
	if tag == "" {
		return "", fmt.Errorf("no tags found")
	}

	return tag, nil
}

func (p *CodebergProvider) getDefaultBranch(repo, repoPath string) string {
	return ResolveGitDefaultBranch(p.getRepoURL(repo), repoPath)
}

func (p *CodebergProvider) createSymlinks(_ string, repoPath string) error {
	nvpmBinDir := files.GetAppBinPath()

	// Look for common binary locations
	binDirs := []string{
		filepath.Join(repoPath, "bin"),
		filepath.Join(repoPath, "target", "release"),
		filepath.Join(repoPath, "dist"),
		repoPath, // Root directory
	}

	for _, binDir := range binDirs {
		if entries, err := codebergReadDir(binDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				// Check if it's executable or looks like a binary
				binPath := filepath.Join(binDir, entry.Name())
				if info, err := codebergStat(binPath); err == nil {
					// Skip hidden files and common non-binary files
					if strings.HasPrefix(entry.Name(), ".") {
						continue
					}
					// Create symlink
					symlink := filepath.Join(nvpmBinDir, entry.Name())
					// Remove existing symlink if it exists
					if _, err := codebergLstat(symlink); err == nil {
						codebergRemove(symlink)
					}
					// Create relative symlink
					relPath, err := filepath.Rel(nvpmBinDir, binPath)
					if err != nil {
						relPath = binPath
					}
					if err := codebergSymlink(relPath, symlink); err != nil {
						Logger.Info(fmt.Sprintf("Codeberg: Warning creating symlink %s -> %s: %v", symlink, relPath, err))
					} else {
						Logger.Info(fmt.Sprintf("Codeberg: Created symlink %s -> %s", symlink, relPath))
					}
					// Only process first executable found per directory to avoid clutter
					if info.Mode()&0111 != 0 {
						break
					}
				}
			}
		}
	}

	return nil
}

func (p *CodebergProvider) removeSymlinks(sourceID, repo string) error {
	repoPath := p.getRepoPath(sourceID, repo)
	repoPath = filepath.Clean(repoPath) + string(os.PathSeparator)
	nvpmBinDir := files.GetAppBinPath()

	// Find and remove symlinks that point to this repo
	entries, err := codebergReadDir(nvpmBinDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		symlink := filepath.Join(nvpmBinDir, entry.Name())
		if link, err := codebergLstat(symlink); err == nil {
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
					if err := codebergRemove(symlink); err != nil {
						Logger.Info(fmt.Sprintf("Codeberg: Warning removing symlink %s: %v", symlink, err))
					}
				}
			}
		}
	}

	return nil
}

func (p *CodebergProvider) Sync() bool {
	Logger.Info("Codeberg Sync: Syncing Codeberg packages")
	localPackages := lppCodebergGetDataForProvider(p.PROVIDER_NAME).Packages

	allOk := true
	for _, pkg := range localPackages {
		repo := p.getRepo(pkg.SourceID)
		if repo == "" {
			continue
		}
		repoPath := p.getRepoPath(pkg.SourceID, repo)
		if _, err := codebergStat(repoPath); os.IsNotExist(err) {
			// Re-install missing packages at the lockfile commit when present.
			Logger.Info(fmt.Sprintf("Codeberg Sync: Re-installing missing package %s", repo))
			SetLockedCommit(pkg.Commit)
			ok := p.Install(pkg.SourceID, pkg.Version)
			ResetLockedCommit()
			if !ok {
				allOk = false
			}
		} else if strings.TrimSpace(pkg.Commit) != "" && gitWorkTreeExists(repoPath) {
			// Existing git clone: restore the pinned commit (branch versions must not float to tip).
			Logger.Info(fmt.Sprintf("Codeberg Sync: Restoring locked commit for %s", repo))
			SetLockedCommit(pkg.Commit)
			ok := p.Install(pkg.SourceID, pkg.Version)
			ResetLockedCommit()
			if !ok {
				allOk = false
			}
		} else if !IsEditorPluginPackage(pkg.SourceID) {
			// Update symlinks
			if err := p.createSymlinks(repo, repoPath); err != nil {
				Logger.Info(fmt.Sprintf("Codeberg Sync: Warning creating symlinks for %s: %v", repo, err))
			}
		}
	}

	return allOk
}

// getLatestReleaseTag gets the latest release tag from Codeberg API (Gitea)
func (p *CodebergProvider) getLatestReleaseTag(repo string) (string, error) {
	apiURL := fmt.Sprintf("https://codeberg.org/api/v1/repos/%s/releases", repo)
	resp, err := codebergHTTPGet(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("codeberg API returned status %d", resp.StatusCode)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	if len(releases) == 0 {
		return "", fmt.Errorf("no releases found")
	}

	return releases[0].TagName, nil
}

// downloadAsset downloads a file from a URL to a destination path
func (p *CodebergProvider) downloadAsset(url, destPath string) error {
	resp, err := codebergHTTPGet(url)
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
func (p *CodebergProvider) extractArchive(archivePath, destDir string) error {
	ext := filepath.Ext(archivePath)
	baseExt := filepath.Ext(strings.TrimSuffix(archivePath, ext))

	if baseExt == ".tar" && ext == ".gz" {
		// Extract tar.gz
		code, err := codebergShellOut("tar", []string{"-xzf", archivePath, "-C", destDir}, "", nil)
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
		code, err := codebergShellOut("sh", []string{"-c", fmt.Sprintf("gunzip -c %s > %s", archivePath, outputPath)}, "", nil)
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
