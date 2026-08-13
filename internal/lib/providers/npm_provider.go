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
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

// Injectable shell and OS helpers for tests
var npmShellOut = shell_out.ShellOut
var npmShellOutCapture = shell_out.ShellOutCapture
var npmCreate = os.Create
var npmReadFile = os.ReadFile
var npmReadDir = os.ReadDir
var npmLstat = os.Lstat
var npmReadlink = os.Readlink
var npmRemove = os.Remove
var npmRemoveAll = os.RemoveAll
var npmSymlink = os.Symlink
var npmChmod = os.Chmod
var npmStat = os.Stat
var npmMkdirAll = os.MkdirAll
var npmClose = func(f *os.File) error { return f.Close() }

// npmQuietEnv suppresses npm's update notifier so its notices cannot pollute
// captured output (e.g. `npm view … version`) or any remaining inherited I/O.
func npmQuietEnv() []string {
	return []string{
		"NO_UPDATE_NOTIFIER=1",
		"npm_config_update_notifier=false",
	}
}

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

// packageDir is the per-package container under packages/npm/<name>.
// Scoped names like @vue/language-server become packages/npm/@vue/language-server.
func (p *NPMProvider) packageDir(packageName string) string {
	return filepath.Join(p.APP_PACKAGES_DIR, packageName)
}

func (p *NPMProvider) nodeModulesDir(packageName string) string {
	return filepath.Join(p.packageDir(packageName), "node_modules")
}

func (p *NPMProvider) installedPackagePath(packageName string) string {
	return filepath.Join(p.nodeModulesDir(packageName), packageName)
}

func (p *NPMProvider) writePackageJSON(dir, name, version string, extras *local_packages_parser.PackageExtras) bool {
	deps := map[string]string{name: version}
	for extraName, extraVer := range npmExtraPackageDependencies(p.PREFIX, extras) {
		if extraName == name {
			continue
		}
		deps[extraName] = extraVer
	}
	packageJSON := struct {
		Dependencies map[string]string `json:"dependencies"`
	}{
		Dependencies: deps,
	}

	filePath := filepath.Join(dir, "package.json")
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
	if err := encoder.Encode(packageJSON); err != nil {
		fmt.Println("Error encoding package.json:", err)
		return false
	}
	return true
}

func npmExtraPackageDependencies(prefix string, extras *local_packages_parser.PackageExtras) map[string]string {
	if extras == nil {
		return nil
	}
	out := make(map[string]string)
	for _, pin := range extras.ExtraPackages {
		id := normalizePackageID(strings.TrimSpace(pin.ID))
		if prefix != "" && !strings.HasPrefix(id, prefix) {
			continue
		}
		extraName := strings.TrimPrefix(id, prefix)
		if extraName == "" {
			continue
		}
		ver := strings.TrimSpace(pin.Version)
		if ver == "" {
			ver = "*"
		}
		out[extraName] = ver
	}
	return out
}

func (p *NPMProvider) generatePackageJSON() bool {
	found := false
	localPackages := lppGetData(true).Packages
	for _, pkg := range localPackages {
		if detectProvider(pkg.SourceID) != ProviderNPM {
			continue
		}
		name := p.getRepo(pkg.SourceID)
		if name == "" {
			continue
		}
		dir := p.packageDir(name)
		if err := npmMkdirAll(dir, 0755); err != nil {
			fmt.Println("error creating directory:", err)
			return false
		}
		if !p.writePackageJSON(dir, name, pkg.Version, pkg.Extras) {
			return false
		}
		found = true
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
	desired := lppGetDataForProvider("npm").Packages
	for _, pkg := range desired {
		name := p.getRepo(pkg.SourceID)
		if name == "" {
			continue
		}
		_ = p.removePackageSymlinks(name)
	}
	return p.removeDanglingLegacyNpmSymlinks()
}

func (p *NPMProvider) removeDanglingLegacyNpmSymlinks() error {
	binDir := files.GetAppBinPath()
	entries, err := npmReadDir(binDir)
	if err != nil {
		return err
	}
	legacyPrefix := filepath.Join(p.APP_PACKAGES_DIR, "node_modules")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		symlinkPath := filepath.Join(binDir, entry.Name())
		target, err := npmReadlink(symlinkPath)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(binDir, target)
		}
		target = filepath.Clean(target)
		if target == legacyPrefix || strings.HasPrefix(target, legacyPrefix+string(os.PathSeparator)) {
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
		if err := npmMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
			fmt.Println("error creating directory:", err)
			return false
		}
	}
	Logger.Info("npm sync: Starting sync process")
	desired := lppGetDataForProvider("npm").Packages
	allOk := true
	installedCount := 0
	skippedCount := 0
	for _, pkg := range desired {
		name := p.getRepo(pkg.SourceID)
		if name == "" {
			continue
		}
		dir := p.packageDir(name)
		if err := npmMkdirAll(dir, 0755); err != nil {
			Logger.Info(fmt.Sprintf("error creating directory %s: %v", dir, err))
			allOk = false
			continue
		}
		if !p.writePackageJSON(dir, name, pkg.Version, pkg.Extras) {
			allOk = false
			continue
		}
		if p.isPackageInstalled(name, pkg.Version) {
			Logger.Info(fmt.Sprintf("npm sync: Package %s@%s already installed, skipping", name, pkg.Version))
			skippedCount++
			if err := p.createPackageSymlinks(name); err != nil {
				Logger.Info(fmt.Sprintf("error creating symlinks for %s: %v", name, err))
			}
			continue
		}
		Logger.Info(fmt.Sprintf("npm sync: Installing package %s@%s", name, pkg.Version))
		if p.tryNpmCiIn(dir) {
			installedCount++
			if err := p.createPackageSymlinks(name); err != nil {
				Logger.Info(fmt.Sprintf("Error creating symlinks for %s: %v", name, err))
			}
			continue
		}
		installCode, err := npmShellOut("npm", []string{"install", "--no-update-notifier", name + "@" + pkg.Version}, dir, npmQuietEnv())
		if err != nil || installCode != 0 {
			Logger.Info(fmt.Sprintf("error installing %s@%s: %v", name, pkg.Version, err))
			allOk = false
			continue
		}
		installedCount++
		if err := p.createPackageSymlinks(name); err != nil {
			Logger.Info(fmt.Sprintf("Error creating symlinks for %s: %v", name, err))
		}
	}
	p.cleanupLegacyNpmRoot()
	Logger.Info(fmt.Sprintf("npm sync: Completed - %d packages installed, %d packages skipped", installedCount, skippedCount))
	return allOk
}

func (p *NPMProvider) cleanupLegacyNpmRoot() {
	for _, name := range []string{"package.json", "package-lock.json"} {
		path := filepath.Join(p.APP_PACKAGES_DIR, name)
		if _, err := npmStat(path); err == nil {
			if err := npmRemove(path); err != nil {
				Logger.Info(fmt.Sprintf("warning: failed to remove leftover %s: %v", path, err))
			}
		}
	}
	legacyNodeModules := filepath.Join(p.APP_PACKAGES_DIR, "node_modules")
	if info, err := npmStat(legacyNodeModules); err == nil && info.IsDir() {
		if err := npmRemoveAll(legacyNodeModules); err != nil {
			Logger.Info(fmt.Sprintf("warning: failed to remove leftover %s: %v", legacyNodeModules, err))
		}
	}
	_ = p.removeDanglingLegacyNpmSymlinks()
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
	packagePath := p.installedPackagePath(packageName)
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return false
	}
	pkg, err := p.readPackageJSON(packagePath)
	if err != nil {
		return false
	}
	return pkg.Version == expectedVersion
}

// normalizeNpmBinCommand turns registry bin values like "npm:tsc" into the
// node_modules/.bin entry name "tsc".
func (p *NPMProvider) normalizeNpmBinCommand(commandToExec string) string {
	commandToExec = strings.TrimSpace(commandToExec)
	if strings.HasPrefix(commandToExec, p.PREFIX) {
		return strings.TrimPrefix(commandToExec, p.PREFIX)
	}
	return commandToExec
}

// resolveNpmSourceID finds the local package source ID for a package name, or
// falls back to npm:<name>.
func (p *NPMProvider) resolveNpmSourceID(packageName string) string {
	for _, pkg := range lppGetDataForProvider("npm").Packages {
		if p.getRepo(pkg.SourceID) == packageName {
			return pkg.SourceID
		}
	}
	return p.PREFIX + packageName
}

// packageBinMap returns symlinkName → node_modules/.bin target name.
// Prefers the nvpm registry bin map (supports aliases like tsgo → tsc);
// falls back to package.json bins when the registry has none.
func (p *NPMProvider) packageBinMap(packageName string) (map[string]string, error) {
	parser := registry_parser.NewDefaultRegistryParser()
	registryItem := parser.GetBySourceId(p.resolveNpmSourceID(packageName))
	if len(registryItem.Bin) > 0 {
		bins := make(map[string]string, len(registryItem.Bin))
		for binName, binCmd := range registryItem.Bin {
			target := p.normalizeNpmBinCommand(binCmd)
			if target == "" {
				continue
			}
			bins[binName] = target
		}
		return bins, nil
	}

	pkg, err := p.readPackageJSON(p.installedPackagePath(packageName))
	if err != nil {
		return nil, fmt.Errorf("error reading package.json for %s: %v", packageName, err)
	}
	bins := make(map[string]string, len(pkg.Bin))
	for binName := range pkg.Bin {
		bins[binName] = binName
	}
	return bins, nil
}

func (p *NPMProvider) createPackageSymlinks(packageName string) error {
	bins, err := p.packageBinMap(packageName)
	if err != nil {
		return err
	}
	if len(bins) == 0 {
		return nil
	}
	nodeModulesPath := p.nodeModulesDir(packageName)
	binDir := files.GetAppBinPath()
	for binName, targetName := range bins {
		actualBinPath := filepath.Join(nodeModulesPath, ".bin", targetName)
		symlinkPath := filepath.Join(binDir, binName)
		if _, err := npmLstat(symlinkPath); err == nil {
			if err := npmRemove(symlinkPath); err != nil {
				Logger.Info(fmt.Sprintf("warning: failed to remove existing symlink %s: %v", symlinkPath, err))
			}
		}
		Logger.Info(fmt.Sprintf("Creating symlink for %s -> %s\n", symlinkPath, actualBinPath))
		if err := npmSymlink(actualBinPath, symlinkPath); err != nil {
			Logger.Info(fmt.Sprintf("error creating symlink for %s: %v", binName, err))
			return err
		}
		if err := npmChmod(symlinkPath, 0755); err != nil {
			Logger.Info(fmt.Sprintf("error setting executable permissions for %s: %v", binName, err))
		}
	}
	return nil
}

func (p *NPMProvider) removePackageSymlinks(packageName string) error {
	bins, err := p.packageBinMap(packageName)
	if err != nil {
		return nil
	}
	binDir := files.GetAppBinPath()
	for binName := range bins {
		symlinkPath := filepath.Join(binDir, binName)
		if _, err := npmLstat(symlinkPath); err == nil {
			if err := npmRemove(symlinkPath); err != nil {
				Logger.Info(fmt.Sprintf("warning: failed to remove symlink %s: %v", symlinkPath, err))
			}
		}
	}
	return nil
}

func (p *NPMProvider) pruneEmptyScopeDir(packageName string) {
	if !strings.HasPrefix(packageName, "@") {
		return
	}
	parent := filepath.Dir(p.packageDir(packageName))
	if parent == p.APP_PACKAGES_DIR {
		return
	}
	entries, err := npmReadDir(parent)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		if err := npmRemove(parent); err != nil {
			Logger.Info(fmt.Sprintf("warning: failed to remove empty scope dir %s: %v", parent, err))
		}
	}
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
	if err := npmRemoveAll(p.packageDir(packageName)); err != nil {
		Logger.Info(fmt.Sprintf("warning: failed to remove package directory %s: %v", p.packageDir(packageName), err))
	}
	p.pruneEmptyScopeDir(packageName)
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
	_, output, err := npmShellOutCapture("npm", []string{"view", packageName, "version", "--no-update-notifier"}, "", npmQuietEnv())
	if err != nil {
		Logger.Error(fmt.Sprintf("npm getLatestVersion: Command failed for %s: %v, output: %s", packageName, err, output))
		return "", err
	}
	return firstNonNoticeLine(output), nil
}

// firstNonNoticeLine returns the first non-empty line that is not an npm notice,
// so CombinedOutput noise cannot be mistaken for a version string.
func firstNonNoticeLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "npm notice") {
			continue
		}
		return line
	}
	return ""
}

func (p *NPMProvider) tryNpmCi() bool {
	return p.tryNpmCiIn(p.APP_PACKAGES_DIR)
}

func (p *NPMProvider) tryNpmCiIn(packageDir string) bool {
	lockFile := filepath.Join(packageDir, "package-lock.json")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		Logger.Info("npm Sync: No package-lock.json found, cannot use npm ci")
		return false
	}
	Logger.Info("npm sync: Using npm ci for faster bulk installation")
	installCode, err := npmShellOut("npm", []string{"ci", "--no-update-notifier"}, packageDir, npmQuietEnv())
	if err != nil || installCode != 0 {
		Logger.Info(fmt.Sprintf("npm sync: npm ci failed, falling back to individual package installation: %v", err))
		return false
	}
	Logger.Info("npm sync: npm ci completed successfully, creating symlinks")
	return true
}

func (p *NPMProvider) hasPackageJSONChanged() bool {
	return p.hasPackageJSONChangedIn(p.APP_PACKAGES_DIR)
}

func (p *NPMProvider) hasPackageJSONChangedIn(dir string) bool {
	packageJSONFile := filepath.Join(dir, "package.json")
	lockFile := filepath.Join(dir, "package-lock.json")
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
