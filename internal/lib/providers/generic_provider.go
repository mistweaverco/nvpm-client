package providers

import (
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

type GenericProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

// Injectable shell and OS helpers for tests
var genericShellOut = shell_out.ShellOut
var genericStat = os.Stat
var genericMkdirAll = os.MkdirAll
var genericLstat = os.Lstat
var genericRemove = os.Remove
var genericRemoveAll = os.RemoveAll
var genericSymlink = os.Symlink
var genericReadDir = os.ReadDir
var genericChmod = os.Chmod

// Injectable local packages helpers for tests
var lppGenericAdd = local_packages_parser.AddLocalPackage
var lppGenericRemove = local_packages_parser.RemoveLocalPackage
var lppGenericGetDataForProvider = local_packages_parser.GetDataForProvider

// Injectable registry parser for tests
var genericRegistryParser = registry_parser.NewDefaultRegistryParser

// Injectable HTTP client for tests
var genericHTTPGet = http.Get

func NewProviderGeneric() *GenericProvider {
	p := &GenericProvider{}
	p.PROVIDER_NAME = "generic"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *GenericProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:generic/pkg) and new (generic:pkg) formats
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

func (p *GenericProvider) Install(sourceID, version string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("Generic Install: Invalid source ID format")
		return false
	}

	// Check registry for download information
	registry := genericRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)

	if len(registryItem.Source.Download) == 0 {
		Logger.Error("Generic Install: No download information found in registry")
		return false
	}

	// Find matching download for current platform
	download := p.findMatchingDownload(registryItem.Source.Download)
	if download == nil {
		Logger.Error("Generic Install: No matching download found for current platform")
		return false
	}

	// Resolve version
	resolvedVersion := version
	if resolvedVersion == "" || resolvedVersion == "latest" {
		resolvedVersion = registryItem.Version
		if resolvedVersion == "" {
			resolvedVersion = "latest"
		}
	}

	// Ensure packages directory exists
	if err := genericMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Generic Install: Error creating packages directory: %v", err))
		return false
	}

	// Create package directory
	packageDir := filepath.Join(p.APP_PACKAGES_DIR, packageName)
	if err := genericMkdirAll(packageDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Generic Install: Error creating package directory: %v", err))
		return false
	}

	// Download and extract files
	extractDir := filepath.Join(packageDir, "extracted")
	if err := genericMkdirAll(extractDir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Generic Install: Error creating extract directory: %v", err))
		return false
	}

	// Download each file
	for filename, url := range download.Files {
		// Resolve template variables in URL
		resolvedURL := ResolveTemplate(url, resolvedVersion)

		Logger.Info(fmt.Sprintf("Generic Install: Downloading %s from %s", filename, resolvedURL))

		// Download file
		filePath := filepath.Join(extractDir, filename)
		if err := p.downloadFile(resolvedURL, filePath); err != nil {
			Logger.Error(fmt.Sprintf("Generic Install: Error downloading %s: %v", filename, err))
			return false
		}

		// Extract if it's an archive
		if strings.HasSuffix(filename, ".zip") || strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tar") {
			extractSubDir := filepath.Join(extractDir, strings.TrimSuffix(filename, filepath.Ext(filename)))
			if err := genericMkdirAll(extractSubDir, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Generic Install: Error creating extract subdirectory: %v", err))
				return false
			}

			if err := p.extractArchive(filePath, extractSubDir); err != nil {
				Logger.Error(fmt.Sprintf("Generic Install: Error extracting %s: %v", filename, err))
				return false
			}

			// Remove archive after extraction
			_ = genericRemove(filePath)
		}
	}

	// Create symlinks
	if err := p.createSymlinksFromRegistry(packageName, extractDir, download, registryItem); err != nil {
		Logger.Info(fmt.Sprintf("Generic Install: Warning creating symlinks: %v", err))
	}

	// Add to local packages
	if err := lppGenericAdd(sourceID, resolvedVersion); err != nil {
		Logger.Error(fmt.Sprintf("Generic Install: Error adding package to local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("Generic Install: Successfully installed %s@%s", packageName, resolvedVersion))
	return true
}

func (p *GenericProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("Generic Remove: Invalid source ID format")
		return false
	}

	// Remove Neovim tree-sitter parser(s) if this package installed them.
	registry := genericRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)
	if err := removeNeovimTreeSitterParsers(registryItem); err != nil {
		Logger.Info(fmt.Sprintf("Generic Remove: Warning removing Neovim tree-sitter parsers: %v", err))
	}

	Logger.Info(fmt.Sprintf("Generic Remove: Removing %s", packageName))

	packageDir := filepath.Join(p.APP_PACKAGES_DIR, packageName)

	// Remove symlinks
	if err := p.removeSymlinks(packageName); err != nil {
		Logger.Info(fmt.Sprintf("Generic Remove: Warning removing symlinks: %v", err))
	}

	// Remove package directory
	if _, err := genericStat(packageDir); err == nil {
		if err := genericRemoveAll(packageDir); err != nil {
			Logger.Error(fmt.Sprintf("Generic Remove: Error removing package directory: %v", err))
			return false
		}
	}

	// Remove from local packages
	if err := lppGenericRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Generic Remove: Error removing package from local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("Generic Remove: Successfully removed %s", packageName))
	return true
}

func (p *GenericProvider) Update(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("Generic Update: Invalid source ID format")
		return false
	}

	// Generic packages use version from registry
	registry := genericRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)
	latestVersion := registryItem.Version

	if latestVersion == "" {
		Logger.Error("Generic Update: No version information in registry")
		return false
	}

	Logger.Info(fmt.Sprintf("Generic Update: Updating %s to version %s", packageName, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *GenericProvider) getLatestVersion(packageName string) (string, error) {
	// Generic provider gets version from registry, not from API
	registry := genericRegistryParser()
	registryItem := registry.GetBySourceId(p.PREFIX + packageName)
	if registryItem.Version != "" {
		return registryItem.Version, nil
	}
	return "", fmt.Errorf("version not found in registry")
}

// findMatchingDownload finds the download entry that matches the current platform
func (p *GenericProvider) findMatchingDownload(downloads registry_parser.RegistryItemSourceDownloadList) *registry_parser.RegistryItemSourceDownloadFile {
	currentTarget := DetectRegistryTarget()

	for i := range downloads {
		if MatchesTarget(downloads[i].Target, currentTarget) {
			return &downloads[i]
		}
	}

	// Try fallback: check for linux_x64_gnu if linux_x64 not found
	if strings.HasPrefix(currentTarget, "linux_") {
		fallbackTarget := currentTarget + "_gnu"
		for i := range downloads {
			if MatchesTarget(downloads[i].Target, fallbackTarget) {
				return &downloads[i]
			}
		}
	}

	return nil
}

// downloadFile downloads a file from a URL to a destination path
func (p *GenericProvider) downloadFile(url, destPath string) error {
	resp, err := genericHTTPGet(url)
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
func (p *GenericProvider) extractArchive(archivePath, destDir string) error {
	ext := filepath.Ext(archivePath)
	baseExt := filepath.Ext(strings.TrimSuffix(archivePath, ext))

	if baseExt == ".tar" && ext == ".gz" {
		// Extract tar.gz
		code, err := genericShellOut("tar", []string{"-xzf", archivePath, "-C", destDir}, "", nil)
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
		code, err := genericShellOut("sh", []string{"-c", fmt.Sprintf("gunzip -c %s > %s", archivePath, outputPath)}, "", nil)
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

// createSymlinksFromRegistry creates symlinks based on registry bin configuration
func (p *GenericProvider) createSymlinksFromRegistry(packageName, extractDir string, download *registry_parser.RegistryItemSourceDownloadFile, registryItem registry_parser.RegistryItem) error {
	nvpmBinDir := files.GetAppBinPath()

	for binName, binTemplate := range registryItem.Bin {
		// Resolve bin path template (e.g., "{{source.download.bin}}")
		binPath := binTemplate
		if strings.Contains(binPath, "{{source.download.bin}}") {
			binPath = strings.ReplaceAll(binPath, "{{source.download.bin}}", download.Bin)
		}

		if binPath == "" {
			continue
		}

		// Find the actual binary file in extracted directory
		binaryFile := filepath.Join(extractDir, binPath)
		if _, err := genericStat(binaryFile); err != nil {
			// Try to find by name recursively
			if found := p.findBinaryInDir(extractDir, filepath.Base(binPath)); found != "" {
				binaryFile = found
			} else {
				continue
			}
		}

		// Make executable if it's a script
		if strings.HasSuffix(binaryFile, ".sh") || strings.HasSuffix(binaryFile, ".py") {
			_ = genericChmod(binaryFile, 0755)
		}

		// Create symlink
		symlink := filepath.Join(nvpmBinDir, binName)
		if _, err := genericLstat(symlink); err == nil {
			genericRemove(symlink)
		}

		relPath, err := filepath.Rel(nvpmBinDir, binaryFile)
		if err != nil {
			relPath = binaryFile
		}

		if err := genericSymlink(relPath, symlink); err != nil {
			Logger.Info(fmt.Sprintf("Generic: Warning creating symlink %s -> %s: %v", symlink, relPath, err))
		} else {
			Logger.Info(fmt.Sprintf("Generic: Created symlink %s -> %s", symlink, relPath))
		}
	}

	return nil
}

// findBinaryInDir searches for a binary file in a directory recursively
func (p *GenericProvider) findBinaryInDir(dir, name string) string {
	entries, err := genericReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if found := p.findBinaryInDir(path, name); found != "" {
				return found
			}
		} else if entry.Name() == name {
			return path
		}
	}

	return ""
}

// removeSymlinks removes symlinks for a specific package
func (p *GenericProvider) removeSymlinks(packageName string) error {
	nvpmBinDir := files.GetAppBinPath()
	packageDir := filepath.Join(p.APP_PACKAGES_DIR, packageName)

	entries, err := genericReadDir(nvpmBinDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		symlink := filepath.Join(nvpmBinDir, entry.Name())
		if link, err := genericLstat(symlink); err == nil {
			if link.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(symlink)
				if err != nil {
					continue
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(nvpmBinDir, target)
				}
				if strings.HasPrefix(target, packageDir) {
					if err := genericRemove(symlink); err != nil {
						Logger.Info(fmt.Sprintf("Generic: Warning removing symlink %s: %v", symlink, err))
					}
				}
			}
		}
	}

	return nil
}

func (p *GenericProvider) Sync() bool {
	Logger.Info("Generic Sync: Syncing generic packages")
	localPackages := lppGenericGetDataForProvider(p.PROVIDER_NAME).Packages

	allOk := true
	for _, pkg := range localPackages {
		packageName := p.getRepo(pkg.SourceID)
		if packageName == "" {
			continue
		}
		packageDir := filepath.Join(p.APP_PACKAGES_DIR, packageName)
		if _, err := genericStat(packageDir); os.IsNotExist(err) {
			// Re-install missing packages
			Logger.Info(fmt.Sprintf("Generic Sync: Re-installing missing package %s", packageName))
			if !p.Install(pkg.SourceID, pkg.Version) {
				allOk = false
			}
		}
	}

	return allOk
}
