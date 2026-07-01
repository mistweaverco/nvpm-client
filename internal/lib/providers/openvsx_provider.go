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
)

type OpenVSXProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
	BASE_URL         string
}

// Injectable shell and OS helpers for tests
var openvsxStat = os.Stat
var openvsxMkdirAll = os.MkdirAll
var openvsxLstat = os.Lstat
var openvsxRemove = os.Remove
var openvsxRemoveAll = os.RemoveAll
var openvsxSymlink = os.Symlink
var openvsxReadDir = os.ReadDir

// Injectable local packages helpers for tests
var lppOpenvsxAdd = local_packages_parser.AddLocalPackage
var lppOpenvsxRemove = local_packages_parser.RemoveLocalPackage
var lppOpenvsxGetDataForProvider = local_packages_parser.GetDataForProvider

// Injectable registry parser for tests
var openvsxRegistryParser = registry_parser.NewDefaultRegistryParser

// Injectable HTTP client for tests
var openvsxHTTPGet = http.Get

func NewProviderOpenVSX() *OpenVSXProvider {
	p := &OpenVSXProvider{}
	p.PROVIDER_NAME = "openvsx"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	p.BASE_URL = "https://open-vsx.org"
	return p
}

func (p *OpenVSXProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:openvsx/publisher/extension) and new (openvsx:publisher/extension) formats
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

func (p *OpenVSXProvider) getExtensionPath(publisher, extension string) string {
	// Sanitize for filesystem
	safePath := strings.ReplaceAll(fmt.Sprintf("%s_%s", publisher, extension), "/", "_")
	return filepath.Join(p.APP_PACKAGES_DIR, safePath)
}

func (p *OpenVSXProvider) Install(sourceID, version string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("OpenVSX Install: Invalid source ID format")
		return false
	}

	// Parse publisher/extension from repo
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		Logger.Error("OpenVSX Install: Invalid format, expected publisher/extension")
		return false
	}
	publisher := parts[0]
	extension := parts[1]

	// Check registry for download information
	registry := openvsxRegistryParser()
	registryItem := registry.GetBySourceId(sourceID)

	// Resolve version
	resolvedVersion := version
	if resolvedVersion == "" || resolvedVersion == "latest" {
		resolvedVersion = registryItem.Version
		if resolvedVersion == "" {
			// Try to get latest version from OpenVSX API
			latestVersion, err := p.getLatestVersion(repo)
			if err != nil {
				Logger.Error(fmt.Sprintf("OpenVSX Install: Could not determine latest version: %v", err))
				return false
			}
			resolvedVersion = latestVersion
		}
	}

	// Get download file name from registry
	var vsixFileName string
	if len(registryItem.Source.Asset) > 0 {
		// Use asset file from registry
		asset := FindMatchingAsset(registryItem.Source.Asset)
		if asset != nil {
			vsixFileName = ResolveTemplate(asset.File.String(), resolvedVersion)
		}
	} else {
		// Fallback: construct filename from publisher/extension
		vsixFileName = fmt.Sprintf("%s.%s-%s.vsix", publisher, extension, resolvedVersion)
	}

	// Download VSIX file
	downloadURL := fmt.Sprintf("%s/api/%s/%s/%s/file/%s", p.BASE_URL, publisher, extension, resolvedVersion, vsixFileName)
	Logger.Info(fmt.Sprintf("OpenVSX Install: Downloading extension from %s", downloadURL))

	// Ensure packages directory exists
	if err := openvsxMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Install: Error creating packages directory: %v", err))
		return false
	}

	// Create temporary directory for download
	extractPath := p.getExtensionPath(publisher, extension)
	if err := openvsxMkdirAll(extractPath, 0755); err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Install: Error creating extract directory: %v", err))
		return false
	}

	// Download VSIX file
	vsixPath := filepath.Join(extractPath, vsixFileName)
	if err := p.downloadFile(downloadURL, vsixPath); err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Install: Error downloading VSIX: %v", err))
		return false
	}

	// Extract VSIX (it's a ZIP file)
	if err := files.Unzip(vsixPath, extractPath); err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Install: Error extracting VSIX: %v", err))
		return false
	}

	// Remove VSIX file after extraction
	_ = openvsxRemove(vsixPath)

	// Create symlinks for binaries
	if err := p.createSymlinksFromRegistry(publisher, extension, extractPath, registryItem); err != nil {
		Logger.Info(fmt.Sprintf("OpenVSX Install: Warning creating symlinks: %v", err))
	}

	// Add to local packages
	if err := lppOpenvsxAdd(sourceID, resolvedVersion); err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Install: Error adding package to local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("OpenVSX Install: Successfully installed %s@%s", repo, resolvedVersion))
	return true
}

func (p *OpenVSXProvider) Remove(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("OpenVSX Remove: Invalid source ID format")
		return false
	}

	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		Logger.Error("OpenVSX Remove: Invalid format")
		return false
	}
	publisher := parts[0]
	extension := parts[1]

	Logger.Info(fmt.Sprintf("OpenVSX Remove: Removing %s", repo))

	extractPath := p.getExtensionPath(publisher, extension)

	// Remove symlinks
	if err := p.removeSymlinks(repo); err != nil {
		Logger.Info(fmt.Sprintf("OpenVSX Remove: Warning removing symlinks: %v", err))
	}

	// Remove extension directory
	if _, err := openvsxStat(extractPath); err == nil {
		if err := openvsxRemoveAll(extractPath); err != nil {
			Logger.Error(fmt.Sprintf("OpenVSX Remove: Error removing extension directory: %v", err))
			return false
		}
	}

	// Remove from local packages
	if err := lppOpenvsxRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Remove: Error removing package from local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("OpenVSX Remove: Successfully removed %s", repo))
	return true
}

func (p *OpenVSXProvider) Update(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("OpenVSX Update: Invalid source ID format")
		return false
	}

	// Get latest version
	latestVersion, err := p.getLatestVersion(repo)
	if err != nil {
		Logger.Error(fmt.Sprintf("OpenVSX Update: Could not determine latest version: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("OpenVSX Update: Updating %s to version %s", repo, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *OpenVSXProvider) getLatestVersion(repo string) (string, error) {
	apiURL := fmt.Sprintf("%s/api/%s", p.BASE_URL, repo)
	resp, err := openvsxHTTPGet(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch extension info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenVSX API returned status %d", resp.StatusCode)
	}

	var extension struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&extension); err != nil {
		return "", fmt.Errorf("failed to parse extension info: %w", err)
	}

	return extension.Version, nil
}

// downloadFile downloads a file from a URL to a destination path
func (p *OpenVSXProvider) downloadFile(url, destPath string) error {
	resp, err := openvsxHTTPGet(url)
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

// createSymlinksFromRegistry creates symlinks based on registry bin configuration
func (p *OpenVSXProvider) createSymlinksFromRegistry(publisher, extension, extractPath string, registryItem registry_parser.RegistryItem) error {
	nvpmBinDir := files.GetAppBinPath()

	for binName, binTemplate := range registryItem.Bin {
		binPath := ResolveBinPath(binTemplate, nil, binName)
		if binPath == "" {
			continue
		}

		// Handle node: prefix (common in VS Code extensions)
		var execPath string
		if strings.HasPrefix(binPath, "node:") {
			// Extract path after "node:"
			execPath = strings.TrimPrefix(binPath, "node:")
			execPath = filepath.Join(extractPath, execPath)
		} else {
			execPath = filepath.Join(extractPath, binPath)
		}

		// Check if executable exists
		if _, err := openvsxStat(execPath); err != nil {
			// Try to find by name
			if found := p.findBinaryInDir(extractPath, filepath.Base(binPath)); found != "" {
				execPath = found
			} else {
				continue
			}
		}

		// Create symlink
		symlink := filepath.Join(nvpmBinDir, binName)
		if _, err := openvsxLstat(symlink); err == nil {
			openvsxRemove(symlink)
		}

		relPath, err := filepath.Rel(nvpmBinDir, execPath)
		if err != nil {
			relPath = execPath
		}

		if err := openvsxSymlink(relPath, symlink); err != nil {
			Logger.Info(fmt.Sprintf("OpenVSX: Warning creating symlink %s -> %s: %v", symlink, relPath, err))
		} else {
			Logger.Info(fmt.Sprintf("OpenVSX: Created symlink %s -> %s", symlink, relPath))
		}
	}

	return nil
}

// findBinaryInDir searches for a binary file in a directory recursively
func (p *OpenVSXProvider) findBinaryInDir(dir, name string) string {
	entries, err := openvsxReadDir(dir)
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

// removeSymlinks removes symlinks for a specific extension
func (p *OpenVSXProvider) removeSymlinks(repo string) error {
	nvpmBinDir := files.GetAppBinPath()
	extractPath := p.getExtensionPath(strings.Split(repo, "/")[0], strings.Split(repo, "/")[1])

	entries, err := openvsxReadDir(nvpmBinDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		symlink := filepath.Join(nvpmBinDir, entry.Name())
		if link, err := openvsxLstat(symlink); err == nil {
			if link.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(symlink)
				if err != nil {
					continue
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(nvpmBinDir, target)
				}
				if strings.HasPrefix(target, extractPath) {
					if err := openvsxRemove(symlink); err != nil {
						Logger.Info(fmt.Sprintf("OpenVSX: Warning removing symlink %s: %v", symlink, err))
					}
				}
			}
		}
	}

	return nil
}

func (p *OpenVSXProvider) Sync() bool {
	Logger.Info("OpenVSX Sync: Syncing OpenVSX packages")
	localPackages := lppOpenvsxGetDataForProvider(p.PROVIDER_NAME).Packages

	allOk := true
	for _, pkg := range localPackages {
		repo := p.getRepo(pkg.SourceID)
		if repo == "" {
			continue
		}
		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			continue
		}
		extractPath := p.getExtensionPath(parts[0], parts[1])
		if _, err := openvsxStat(extractPath); os.IsNotExist(err) {
			// Re-install missing packages
			Logger.Info(fmt.Sprintf("OpenVSX Sync: Re-installing missing package %s", repo))
			if !p.Install(pkg.SourceID, pkg.Version) {
				allOk = false
			}
		} else {
			// Update symlinks
			registry := openvsxRegistryParser()
			registryItem := registry.GetBySourceId(pkg.SourceID)
			if err := p.createSymlinksFromRegistry(parts[0], parts[1], extractPath, registryItem); err != nil {
				Logger.Info(fmt.Sprintf("OpenVSX Sync: Warning creating symlinks for %s: %v", repo, err))
			}
		}
	}

	return allOk
}
