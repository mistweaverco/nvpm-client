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

type ComposerProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

var composerCmd = "composer"

// Injectable shell and OS helpers for tests
var composerShellOut = shell_out.ShellOut
var composerShellOutCapture = shell_out.ShellOutCapture
var composerHasCommand = shell_out.HasCommand
var composerCreate = os.Create
var composerReadFile = os.ReadFile
var composerLstat = os.Lstat
var composerRemove = os.Remove
var composerChmod = os.Chmod
var composerStat = os.Stat
var composerMkdirAll = os.MkdirAll
var composerRemoveAll = os.RemoveAll
var composerReadDir = os.ReadDir
var composerWriteFile = os.WriteFile
var composerClose = func(f *os.File) error { return f.Close() }

// Injectable local packages helpers for tests
var lppComposerAdd = local_packages_parser.AddLocalPackage
var lppComposerRemove = local_packages_parser.RemoveLocalPackage
var lppComposerGetDataForProvider = local_packages_parser.GetDataForProvider
var lppComposerGetData = local_packages_parser.GetData

func NewProviderComposer() *ComposerProvider {
	p := &ComposerProvider{}
	p.PROVIDER_NAME = "composer"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *ComposerProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:composer/vendor/package) and new (composer:vendor/package) formats
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

func (p *ComposerProvider) packageDir(packageName string) string {
	return filepath.Join(p.APP_PACKAGES_DIR, packageName)
}

func (p *ComposerProvider) writeComposerJSON(dir, packageName, version string) bool {
	composerJSON := struct {
		Require map[string]string `json:"require"`
	}{
		Require: make(map[string]string),
	}
	if version != "" && version != "latest" {
		composerJSON.Require[packageName] = "^" + version
	} else {
		composerJSON.Require[packageName] = "*"
	}

	filePath := filepath.Join(dir, "composer.json")
	file, err := composerCreate(filePath)
	if err != nil {
		Logger.Error(fmt.Sprintf("Error creating composer.json: %s", err))
		return false
	}
	defer func() {
		if closeErr := composerClose(file); closeErr != nil {
			_ = fmt.Errorf("warning: failed to close composer.json: %v", closeErr)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(composerJSON); err != nil {
		Logger.Error(fmt.Sprintf("Error encoding composer.json: %s", err))
		return false
	}
	return true
}

func (p *ComposerProvider) generateComposerJSON() bool {
	found := false
	localPackages := lppComposerGetData(true).Packages
	for _, pkg := range localPackages {
		if detectProvider(pkg.SourceID) != ProviderComposer {
			continue
		}
		packageName := p.getRepo(pkg.SourceID)
		if packageName == "" {
			continue
		}
		dir := p.packageDir(packageName)
		if err := composerMkdirAll(dir, 0755); err != nil {
			Logger.Error(fmt.Sprintf("Error creating directory %s: %s", dir, err))
			return false
		}
		if !p.writeComposerJSON(dir, packageName, pkg.Version) {
			return false
		}
		found = true
	}
	return found
}

func (p *ComposerProvider) Install(sourceID, version string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("Composer Install: Invalid source ID format")
		return false
	}

	if !composerHasCommand("composer", []string{"--version"}, nil) {
		Logger.Error("Composer Install: composer command not found. Please install Composer.")
		return false
	}

	dir := p.packageDir(packageName)
	if err := composerMkdirAll(dir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Composer Install: Error creating packages directory: %v", err))
		return false
	}

	// Build composer require command
	packageSpec := packageName
	if version != "" && version != "latest" {
		packageSpec = fmt.Sprintf("%s:^%s", packageName, version)
	}

	Logger.Info(fmt.Sprintf("Composer Install: Installing %s@%s", packageName, version))
	code, err := composerShellOut(composerCmd, []string{"require", packageSpec, "--no-interaction", "--no-plugins", "--no-scripts"}, dir, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("Composer Install: Error installing package: %v", err))
		return false
	}

	// Get installed version from composer.lock or vendor directory
	installedVersion := version
	if installedVersion == "" || installedVersion == "latest" {
		// Try to read from composer.lock
		lockPath := filepath.Join(dir, "composer.lock")
		if lockData, err := composerReadFile(lockPath); err == nil {
			var lock struct {
				Packages []struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"packages"`
			}
			if err := json.Unmarshal(lockData, &lock); err == nil {
				for _, pkg := range lock.Packages {
					if pkg.Name == packageName {
						installedVersion = strings.TrimPrefix(pkg.Version, "v")
						break
					}
				}
			}
		}
		if installedVersion == "" || installedVersion == "latest" {
			installedVersion = "latest"
		}
	}

	// Add to local packages
	if err := lppComposerAdd(sourceID, installedVersion); err != nil {
		Logger.Error(fmt.Sprintf("Composer Install: Error adding package to local packages: %v", err))
		return false
	}

	// Regenerate composer.json
	_ = p.generateComposerJSON()

	// Create wrappers for binaries
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("Composer Install: Warning creating wrappers: %v", err))
	}
	p.cleanupLegacyComposerRoot()

	Logger.Info(fmt.Sprintf("Composer Install: Successfully installed %s@%s", packageName, installedVersion))
	return true
}

func (p *ComposerProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("Composer Remove: Invalid source ID format")
		return false
	}

	if !composerHasCommand("composer", []string{"--version"}, nil) {
		Logger.Error("Composer Remove: composer command not found. Please install Composer.")
		return false
	}

	Logger.Info(fmt.Sprintf("Composer Remove: Removing %s", packageName))

	// Remove wrappers
	if err := p.removeWrappersForPackage(packageName); err != nil {
		Logger.Info(fmt.Sprintf("Composer Remove: Warning removing wrappers: %v", err))
	}

	dir := p.packageDir(packageName)
	code, err := composerShellOut(composerCmd, []string{"remove", packageName, "--no-interaction", "--no-plugins", "--no-scripts"}, dir, nil)
	if err != nil || code != 0 {
		Logger.Info(fmt.Sprintf("Composer Remove: Warning removing package (may not be installed): %v", err))
	}

	// Remove from local packages
	if err := lppComposerRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Composer Remove: Error removing package from local packages: %v", err))
		return false
	}
	if err := composerRemoveAll(dir); err != nil {
		Logger.Info(fmt.Sprintf("Composer Remove: Warning removing package directory: %v", err))
	}
	p.pruneEmptyVendorDir(packageName)

	// Regenerate composer.json
	_ = p.generateComposerJSON()

	Logger.Info(fmt.Sprintf("Composer Remove: Successfully removed %s", packageName))
	return true
}

func (p *ComposerProvider) Update(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("Composer Update: Invalid source ID format")
		return false
	}

	if !composerHasCommand("composer", []string{"--version"}, nil) {
		Logger.Error("Composer Update: composer command not found. Please install Composer.")
		return false
	}

	Logger.Info(fmt.Sprintf("Composer Update: Updating %s", packageName))

	dir := p.packageDir(packageName)
	code, err := composerShellOut(composerCmd, []string{"update", packageName, "--no-interaction", "--no-plugins", "--no-scripts"}, dir, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("Composer Update: Error updating package: %v", err))
		return false
	}

	// Get updated version
	var updatedVersion string
	lockPath := filepath.Join(dir, "composer.lock")
	if lockData, err := composerReadFile(lockPath); err == nil {
		var lock struct {
			Packages []struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(lockData, &lock); err == nil {
			for _, pkg := range lock.Packages {
				if pkg.Name == packageName {
					updatedVersion = strings.TrimPrefix(pkg.Version, "v")
					break
				}
			}
		}
	}
	if updatedVersion == "" {
		updatedVersion = "latest"
	}

	// Update local packages
	if err := lppComposerRemove(sourceID); err == nil {
		if err := lppComposerAdd(sourceID, updatedVersion); err != nil {
			Logger.Error(fmt.Sprintf("Composer Update: Error updating package in local packages: %v", err))
			return false
		}
	}

	// Regenerate composer.json
	_ = p.generateComposerJSON()

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("Composer Update: Warning recreating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("Composer Update: Successfully updated %s@%s", packageName, updatedVersion))
	return true
}

func (p *ComposerProvider) getLatestVersion(packageName string) (string, error) {
	if !composerHasCommand("composer", []string{"--version"}, nil) {
		return "", fmt.Errorf("composer command not found")
	}

	// Use composer show to get latest version
	code, output, err := composerShellOutCapture(composerCmd, []string{"show", packageName, "--all", "--no-interaction"}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("failed to get package info: %v", err)
	}

	// Parse output to find versions
	// Output format: "versions : 1.0.0, 1.1.0, 2.0.0"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "versions") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				versions := strings.Split(strings.TrimSpace(parts[1]), ",")
				if len(versions) > 0 {
					// Get the last version (latest)
					return strings.TrimSpace(versions[len(versions)-1]), nil
				}
			}
		}
	}

	return "", fmt.Errorf("version not found")
}

func (p *ComposerProvider) findComposerBinDir() string {
	return p.findComposerBinDirIn(p.APP_PACKAGES_DIR)
}

func (p *ComposerProvider) findComposerBinDirIn(prefixDir string) string {
	return filepath.Join(prefixDir, "vendor", "bin")
}

func (p *ComposerProvider) isComposerPackageInstalled(packageName string) bool {
	vendorPkg := filepath.Join(p.packageDir(packageName), "vendor", packageName)
	_, err := composerStat(vendorPkg)
	return err == nil
}

func (p *ComposerProvider) pruneEmptyVendorDir(packageName string) {
	parent := filepath.Dir(p.packageDir(packageName))
	if parent == p.APP_PACKAGES_DIR || parent == "." || parent == string(filepath.Separator) {
		return
	}
	entries, err := composerReadDir(parent)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = composerRemove(parent)
}

// createWrappers creates wrapper scripts for composer executables
func (p *ComposerProvider) createWrappers() error {
	desired := lppComposerGetDataForProvider("composer").Packages
	if len(desired) == 0 {
		return nil
	}
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()
	for _, pkg := range desired {
		registryItem := parser.GetBySourceId(pkg.SourceID)
		if len(registryItem.Bin) == 0 {
			continue
		}
		prefixDir := p.packageDir(p.getRepo(pkg.SourceID))
		for binName, binCmd := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := composerLstat(wrapperPath); err == nil {
				_ = composerRemove(wrapperPath)
			}
			if err := p.createComposerWrapperForPrefix(binCmd, wrapperPath, prefixDir); err != nil {
				Logger.Error(fmt.Sprintf("Error creating wrapper for %s: %v", binName, err))
				continue
			}
			if err := composerChmod(wrapperPath, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Error setting executable permissions for %s: %v", binName, err))
			}
		}
	}
	return nil
}

// createComposerWrapperForCommand creates a wrapper that prepares the environment and executes the given command
func (p *ComposerProvider) createComposerWrapperForCommand(commandToExec string, wrapperPath string) error {
	return p.createComposerWrapperForPrefix(commandToExec, wrapperPath, p.APP_PACKAGES_DIR)
}

func (p *ComposerProvider) createComposerWrapperForPrefix(commandToExec string, wrapperPath string, prefixDir string) error {
	if prefixDir == "" {
		prefixDir = p.APP_PACKAGES_DIR
	}
	composerBinDir := p.findComposerBinDirIn(prefixDir)
	vendorDir := filepath.Join(prefixDir, "vendor")
	if commandToExec == "" {
		return fmt.Errorf("empty command for wrapper %s", wrapperPath)
	}

	// The command might be a path like "composer:binary-name" or just "binary-name"
	var execCmd string
	if strings.HasPrefix(commandToExec, "composer:") {
		binName := strings.TrimPrefix(commandToExec, "composer:")
		execCmd = filepath.Join(composerBinDir, binName)
	} else {
		execCmd = filepath.Join(composerBinDir, commandToExec)
	}

	wrapperContent := fmt.Sprintf(`#!/bin/sh
# Sets up PHP/Composer environment for nvpm-installed packages and runs the target command

# Add the nvpm composer bin directory to PATH
export PATH="%s:$PATH"

# Add composer vendor directory to PHP include path
export COMPOSER_VENDOR_DIR="%s"

# Execute the command from registry
exec %s "$@"
`, composerBinDir, vendorDir, execCmd)

	if err := composerWriteFile(wrapperPath, []byte(wrapperContent), 0755); err != nil {
		return err
	}
	return nil
}

// removeWrappersForPackage removes wrapper scripts for a specific package
func (p *ComposerProvider) removeWrappersForPackage(packageName string) error {
	desired := lppComposerGetDataForProvider("composer").Packages
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()

	for _, pkg := range desired {
		if p.getRepo(pkg.SourceID) != packageName {
			continue
		}
		registryItem := parser.GetBySourceId(pkg.SourceID)
		for binName := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := composerLstat(wrapperPath); err == nil {
				if err := composerRemove(wrapperPath); err != nil {
					Logger.Info(fmt.Sprintf("Composer: Warning removing wrapper %s: %v", wrapperPath, err))
				}
			}
		}
	}
	return nil
}

func (p *ComposerProvider) cleanupLegacyComposerRoot() {
	for _, name := range []string{"composer.json", "composer.lock"} {
		path := filepath.Join(p.APP_PACKAGES_DIR, name)
		if _, err := composerStat(path); err == nil {
			_ = composerRemove(path)
		}
	}
	legacyVendor := filepath.Join(p.APP_PACKAGES_DIR, "vendor")
	if _, err := composerStat(filepath.Join(legacyVendor, "autoload.php")); err == nil {
		_ = composerRemoveAll(legacyVendor)
	}
}

func (p *ComposerProvider) Sync() bool {
	Logger.Info("Composer Sync: Syncing composer packages")
	localPackages := lppComposerGetDataForProvider(p.PROVIDER_NAME).Packages

	if len(localPackages) == 0 {
		p.cleanupLegacyComposerRoot()
		return true
	}

	if !composerHasCommand("composer", []string{"--version"}, nil) {
		Logger.Error("Composer Sync: composer command not found. Please install Composer.")
		return false
	}

	allOk := true
	for _, pkg := range localPackages {
		packageName := p.getRepo(pkg.SourceID)
		if packageName == "" {
			continue
		}
		dir := p.packageDir(packageName)
		if err := composerMkdirAll(dir, 0755); err != nil {
			Logger.Error(fmt.Sprintf("Composer Sync: Error creating directory %s: %v", dir, err))
			allOk = false
			continue
		}
		if !p.writeComposerJSON(dir, packageName, pkg.Version) {
			allOk = false
			continue
		}
		if p.isComposerPackageInstalled(packageName) {
			continue
		}
		packageSpec := packageName
		if pkg.Version != "" && pkg.Version != "latest" {
			packageSpec = fmt.Sprintf("%s:^%s", packageName, pkg.Version)
		}
		code, err := composerShellOut(composerCmd, []string{"require", packageSpec, "--no-interaction", "--no-plugins", "--no-scripts"}, dir, nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("Composer Sync: Error installing %s: %v", packageName, err))
			allOk = false
		}
	}

	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("Composer Sync: Warning creating wrappers: %v", err))
	}
	p.cleanupLegacyComposerRoot()
	return allOk
}
