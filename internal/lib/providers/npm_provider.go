package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

// Injectable shell and OS helpers for tests
var npmShellOut = shell_out.ShellOut
var npmShellOutCapture = shell_out.ShellOutCapture
var npmCreate = os.Create
var npmReadFile = os.ReadFile
var npmReadDir = os.ReadDir
var npmLstat = os.Lstat
var npmRemove = os.Remove
var npmRemoveAll = os.RemoveAll
var npmSymlink = os.Symlink
var npmChmod = os.Chmod
var npmStat = os.Stat
var npmMkdir = os.Mkdir
var npmClose = func(f *os.File) error { return f.Close() }

// Injectable local packages helpers for tests
var lppAdd = local_packages_parser.AddLocalPackage
var lppRemove = local_packages_parser.RemoveLocalPackage
var lppGetData = local_packages_parser.GetData
var lppGetDataForProvider = local_packages_parser.GetDataForProvider

type NPMProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

func NewProviderNPM() *NPMProvider {
	p := &NPMProvider{}
	p.PROVIDER_NAME = "npm"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *NPMProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:npm/pkg) and new (npm:pkg) formats
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

func (p *NPMProvider) generatePackageJSON() bool {
	found := false
	packageJSON := struct {
		Dependencies map[string]string `json:"dependencies"`
	}{
		Dependencies: make(map[string]string),
	}

	localPackages := lppGetData(true).Packages
	for _, pkg := range localPackages {
		if detectProvider(pkg.SourceID) != ProviderNPM {
			continue
		}
		packageJSON.Dependencies[p.getRepo(pkg.SourceID)] = pkg.Version
		found = true
	}

	filePath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	file, err := npmCreate(filePath)
	if err != nil {
		fmt.Println("error creating package.json:", err)
		return false
	}
	defer func() {
		if closeErr := npmClose(file); closeErr != nil {
			fmt.Printf("warning: failed to close package.json file: %v\n", closeErr)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(packageJSON)
	if err != nil {
		fmt.Println("Error encoding package.json:", err)
		return false
	}

	return found
}

type PackageJSON struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Bin     CustomBinField `json:"bin"`
}

type CustomBinField map[string]string

func (cbf *CustomBinField) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		*cbf = m
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		// If it's a string, we assume it's a single binary name
		// and create a map with the binary name as the key and the string as the value.
		// This is a common case for npm packages that have a single binary.
		// Also remove the extension if present.
		binName := strings.TrimSuffix(filepath.Base(s), filepath.Ext(s))
		*cbf = map[string]string{binName: s}
		return nil
	}

	return fmt.Errorf("bin field must be a string or a map")
}

func (p *NPMProvider) readPackageJSON(packagePath string) (*PackageJSON, error) {
	packageJSONPath := filepath.Join(packagePath, "package.json")
	data, err := npmReadFile(packageJSONPath)
	if err != nil {
		return nil, err
	}
	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (p *NPMProvider) removeAllSymlinks() error {
	binDir := files.GetAppBinPath()
	entries, err := npmReadDir(binDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		symlinkPath := filepath.Join(binDir, entry.Name())
		if _, err := npmLstat(symlinkPath); err == nil {
			if err := npmRemove(symlinkPath); err != nil {
				Logger.Info(fmt.Sprintf("warning: failed to remove symlink %s: %v", symlinkPath, err))
			}
		}
	}
	return nil
}

func (p *NPMProvider) Clean() bool {
	if err := p.removeAllSymlinks(); err != nil {
		Logger.Info(fmt.Sprintf("error removing symlinks: %v", err))
	}
	if err := npmRemoveAll(p.APP_PACKAGES_DIR); err != nil {
		Logger.Info(fmt.Sprintf("error removing directory: %v", err))
		return false
	}
	return p.Sync()
}

func (p *NPMProvider) Sync() bool {
	if _, err := npmStat(p.APP_PACKAGES_DIR); os.IsNotExist(err) {
		if err := npmMkdir(p.APP_PACKAGES_DIR, 0755); err != nil {
			fmt.Println("error creating directory:", err)
			return false
		}
	}
	Logger.Info("npm sync: Starting sync process")
	packagesFound := p.generatePackageJSON()
	if !packagesFound {
		return true
	}
	desired := lppGetDataForProvider("npm").Packages
	lockFile := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	packageJSONFile := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	lockExists := false
	lockNewer := false
	if lockStat, err := npmStat(lockFile); err == nil {
		lockExists = true
		if pkgStat, err := npmStat(packageJSONFile); err == nil {
			lockNewer = lockStat.ModTime().After(pkgStat.ModTime())
		}
	}
	// Note: We intentionally unify handling of the fast-path here to avoid
	// duplicated branches that were hard to exercise in tests. The behavior
	// remains the same: when all desired match the lockfile, create symlinks
	// and return true.
	if lockExists && lockNewer {
		installed := p.getInstalledPackagesFromLock(lockFile)
		allInstalled := true
		needsUpdate := false
		for _, pkg := range desired {
			name := p.getRepo(pkg.SourceID)
			if v, ok := installed[name]; !ok || v != pkg.Version {
				allInstalled = false
				needsUpdate = true
				break
			}
		}
		if allInstalled {
			for _, pkg := range desired {
				name := p.getRepo(pkg.SourceID)
				if err := p.createPackageSymlinks(name); err != nil {
					Logger.Info(fmt.Sprintf("error creating symlinks for %s: %v", name, err))
				}
			}
			return true
		}
		if needsUpdate {
			Logger.Info("npm sync: Attempting npm ci for faster bulk installation")
			if p.tryNpmCi() {
				Logger.Info("npm sync: npm ci completed successfully")
				return true
			}
			Logger.Info("npm sync: npm ci failed, falling back to individual package installation")
			if err := npmRemove(lockFile); err != nil {
				Logger.Info(fmt.Sprintf("warning: failed to remove lock file: %v", err))
			}
		}
	}
	Logger.Info("npm sync: Installing packages individually")
	allOk := true
	installedCount := 0
	skippedCount := 0
	for _, pkg := range desired {
		name := p.getRepo(pkg.SourceID)
		if p.isPackageInstalled(name, pkg.Version) {
			Logger.Info(fmt.Sprintf("npm sync: Package %s@%s already installed, skipping", name, pkg.Version))
			skippedCount++
			if err := p.createPackageSymlinks(name); err != nil {
				Logger.Info(fmt.Sprintf("error creating symlinks for %s: %v", name, err))
			}
			continue
		}
		Logger.Info(fmt.Sprintf("npm sync: Installing package %s@%s", name, pkg.Version))
		installCode, err := npmShellOut("npm", []string{"install", name + "@" + pkg.Version}, p.APP_PACKAGES_DIR, nil)
		if err != nil || installCode != 0 {
			fmt.Printf("error installing %s@%s: %v\n", name, pkg.Version, err)
			allOk = false
		} else {
			installedCount++
			if err := p.createPackageSymlinks(name); err != nil {
				Logger.Info(fmt.Sprintf("Error creating symlinks for %s: %v", name, err))
			}
		}
	}
	Logger.Info(fmt.Sprintf("npm sync: Completed - %d packages installed, %d packages skipped", installedCount, skippedCount))
	return allOk
}

func (p *NPMProvider) getInstalledPackagesFromLock(lockFile string) map[string]string {
	installed := map[string]string{}
	data, err := os.ReadFile(lockFile)
	if err != nil {
		return installed
	}
	var lock struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err == nil {
		for pkg, info := range lock.Dependencies {
			installed[pkg] = info.Version
		}
	}
	return installed
}

func (p *NPMProvider) isPackageInstalled(packageName, expectedVersion string) bool {
	packagePath := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", packageName)
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return false
	}
	pkg, err := p.readPackageJSON(packagePath)
	if err != nil {
		return false
	}
	return pkg.Version == expectedVersion
}

func (p *NPMProvider) createPackageSymlinks(packageName string) error {
	nodeModulesPath := filepath.Join(p.APP_PACKAGES_DIR, "node_modules")
	packagePath := filepath.Join(nodeModulesPath, packageName)
	pkg, err := p.readPackageJSON(packagePath)
	if err != nil {
		return fmt.Errorf("error reading package.json for %s: %v", packageName, err)
	}
	if len(pkg.Bin) > 0 {
		binDir := files.GetAppBinPath()
		for binPath := range pkg.Bin {
			actualBinPath := filepath.Join(nodeModulesPath, ".bin", binPath)
			symlinkPath := filepath.Join(binDir, binPath)
			if _, err := npmLstat(symlinkPath); err == nil {
				if err := npmRemove(symlinkPath); err != nil {
					Logger.Info(fmt.Sprintf("warning: failed to remove existing symlink %s: %v", symlinkPath, err))
				}
			}
			Logger.Info(fmt.Sprintf("Creating symlink for %s -> %s\n", symlinkPath, actualBinPath))
			if err := npmSymlink(actualBinPath, symlinkPath); err != nil {
				Logger.Info(fmt.Sprintf("error creating symlink for %s: %v", binPath, err))
				return err
			}
			if err := npmChmod(symlinkPath, 0755); err != nil {
				Logger.Info(fmt.Sprintf("error setting executable permissions for %s: %v", binPath, err))
			}
		}
	}
	return nil
}

func (p *NPMProvider) removePackageSymlinks(packageName string) error {
	binDir := files.GetAppBinPath()
	nodeModulesPath := filepath.Join(p.APP_PACKAGES_DIR, "node_modules")
	packagePath := filepath.Join(nodeModulesPath, packageName)
	pkg, err := p.readPackageJSON(packagePath)
	if err != nil {
		return nil
	}
	for binName := range pkg.Bin {
		symlinkPath := filepath.Join(binDir, binName)
		if _, err := npmLstat(symlinkPath); err == nil {
			if err := npmRemove(symlinkPath); err != nil {
				Logger.Info(fmt.Sprintf("warning: failed to remove symlink %s: %v", symlinkPath, err))
			}
		}
	}
	return nil
}

func (p *NPMProvider) Install(sourceID, version string) bool {
	packageName := p.getRepo(sourceID)
	if version == "" || version == "latest" {
		var err error
		version, err = p.getLatestVersion(packageName)
		if err != nil {
			Logger.Info(fmt.Sprintf("error getting latest version for %s: %v", packageName, err))
			return false
		}
	}
	if err := lppAdd(sourceID, version); err != nil {
		return false
	}
	success := p.Sync()
	if success {
		if err := p.createPackageSymlinks(packageName); err != nil {
			Logger.Info(fmt.Sprintf("error creating symlinks for %s: %v", packageName, err))
		}
	}
	return success
}

func (p *NPMProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	Logger.Info(fmt.Sprintf("npm remove: Removing package %s", packageName))
	_ = p.removePackageSymlinks(packageName)
	if err := lppRemove(sourceID); err != nil {
		Logger.Info(fmt.Sprintf("Error removing package %s from local packages: %v", packageName, err))
		return false
	}
	Logger.Info(fmt.Sprintf("npm remove: Package %s removed successfully", packageName))
	return p.Sync()
}

func (p *NPMProvider) Update(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Info("Invalid source ID format for NPM provider")
		return false
	}
	latestVersion, err := p.getLatestVersion(repo)
	if err != nil {
		Logger.Info(fmt.Sprintf("error getting latest version for %s: %v", repo, err))
		return false
	}
	Logger.Info(fmt.Sprintf("npm update: Updating %s to version %s", repo, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *NPMProvider) getLatestVersion(packageName string) (string, error) {
	_, output, err := npmShellOutCapture("npm", []string{"view", packageName, "version"}, "", nil)
	if err != nil {
		Logger.Error(fmt.Sprintf("npm getLatestVersion: Command failed for %s: %v, output: %s", packageName, err, output))
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (p *NPMProvider) tryNpmCi() bool {
	lockFile := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		Logger.Info("npm Sync: No package-lock.json found, cannot use npm ci")
		return false
	}
	Logger.Info("npm sync: Using npm ci for faster bulk installation")
	installCode, err := npmShellOut("npm", []string{"ci"}, p.APP_PACKAGES_DIR, nil)
	if err != nil || installCode != 0 {
		Logger.Info(fmt.Sprintf("npm sync: npm ci failed, falling back to individual package installation: %v", err))
		return false
	}
	Logger.Info("npm sync: npm ci completed successfully, creating symlinks")
	return true
}

func (p *NPMProvider) hasPackageJSONChanged() bool {
	packageJSONFile := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	lockFile := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	if _, err := npmStat(packageJSONFile); os.IsNotExist(err) {
		return true
	}
	if _, err := npmStat(lockFile); os.IsNotExist(err) {
		return true
	}
	pkgStat, err := npmStat(packageJSONFile)
	if err != nil {
		return true
	}
	lockStat, err := npmStat(lockFile)
	if err != nil {
		return true
	}
	return pkgStat.ModTime().After(lockStat.ModTime())
}
