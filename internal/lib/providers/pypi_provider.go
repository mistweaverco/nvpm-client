package providers

import (
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

type PyPiProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

var pipCmd = "pip"

// Injectable shell and OS helpers for tests
var pipShellOut = shell_out.ShellOut
var pipShellOutCapture = shell_out.ShellOutCapture
var pipHasCommand = shell_out.HasCommand
var pipCreate = os.Create
var pipReadDir = os.ReadDir
var pipReadFile = os.ReadFile
var pipLstat = os.Lstat
var pipRemove = os.Remove
var pipChmod = os.Chmod
var pipStat = os.Stat
var pipMkdir = os.Mkdir
var pipRemoveAll = os.RemoveAll
var pipWriteFile = os.WriteFile
var pipClose = func(f *os.File) error { return f.Close() }

// Injectable local packages helpers for tests
var lppPyAdd = local_packages_parser.AddLocalPackage
var lppPyRemove = local_packages_parser.RemoveLocalPackage
var lppPyGetDataForProvider = local_packages_parser.GetDataForProvider
var lppPyGetData = local_packages_parser.GetData

// Injectable for testing
var pipGetPythonVersion = func(p *PyPiProvider) (string, error) {
	// Try python3 first, then python
	pythonCmd := "python3"
	if !pipHasCommand("python3", []string{"--version"}, nil) {
		if !pipHasCommand("python", []string{"--version"}, nil) {
			return "", fmt.Errorf("python3 or python command not found")
		}
		pythonCmd = "python"
	}

	code, output, err := pipShellOutCapture(pythonCmd, []string{"-c", "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("failed to detect Python version: %v, output: %s", err, output)
	}
	version := strings.TrimSpace(output)
	return version, nil
}

// getPythonVersion detects the current Python version (e.g., "3.11", "3.12")
// by running python -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
func (p *PyPiProvider) getPythonVersion() (string, error) {
	return pipGetPythonVersion(p)
}

func NewProviderPyPi() *PyPiProvider {
	p := &PyPiProvider{}
	p.PROVIDER_NAME = "pypi"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"

	// Prefer pip3 to ensure we're using Python 3, fallback to pip
	hasPip3 := pipHasCommand("pip3", []string{"--version"}, nil)
	if hasPip3 {
		pipCmd = "pip3"
	} else {
		// pipCmd defaults to "pip" if pip3 is not available
		_ = pipHasCommand("pip", []string{"--version"}, nil)
	}
	return p
}

func (p *PyPiProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:pypi/pkg) and new (pypi:pkg) formats
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

func (p *PyPiProvider) generateRequirementsTxt() bool {
	found := false
	dependenciesTxt := make([]string, 0)
	localPackages := lppPyGetData(true).Packages
	for _, pkg := range localPackages {
		if detectProvider(pkg.SourceID) != ProviderPyPi {
			continue
		}
		dependenciesTxt = append(dependenciesTxt, fmt.Sprintf("%s==%s", p.getRepo(pkg.SourceID), pkg.Version))
		found = true
	}
	filePath := filepath.Join(p.APP_PACKAGES_DIR, "requirements.txt")
	file, err := pipCreate(filePath)
	if err != nil {
		Logger.Error(fmt.Sprintf("Error creating requirements.txt: %s", err))
		return false
	}
	for _, line := range dependenciesTxt {
		if _, err := file.WriteString(line + "\n"); err != nil {
			Logger.Error(fmt.Sprintf("Error writing to requirements.txt: %s", err))
			return false
		}
	}
	defer func() {
		if closeErr := pipClose(file); closeErr != nil {
			_ = fmt.Errorf("warning: failed to close requirements.txt file: %v", closeErr)
		}
	}()
	return found
}

// PackageInfo represents the structure of a Python package's metadata
type PackageInfo struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	EntryPoints map[string]interface{} `json:"entry_points,omitempty"`
}

// readPackageInfo reads package metadata from installed Python packages
func (p *PyPiProvider) readPackageInfo(packagePath string) (*PackageInfo, error) {
	entries, err := pipReadDir(packagePath)
	if err != nil {
		return nil, err
	}

	var infoDir string
	for _, entry := range entries {
		if entry.IsDir() && (strings.HasSuffix(entry.Name(), ".dist-info") || strings.HasSuffix(entry.Name(), ".egg-info")) {
			infoDir = filepath.Join(packagePath, entry.Name())
			break
		}
	}
	if infoDir == "" {
		return nil, fmt.Errorf("no package info directory found")
	}

	metadataFiles := []string{"METADATA", "PKG-INFO"}
	var metadataContent string
	for _, filename := range metadataFiles {
		metadataPath := filepath.Join(infoDir, filename)
		if data, err := pipReadFile(metadataPath); err == nil {
			metadataContent = string(data)
			break
		}
	}
	if metadataContent == "" {
		return nil, fmt.Errorf("no metadata file found")
	}

	lines := strings.Split(metadataContent, "\n")
	info := &PackageInfo{}
	for _, line := range lines {
		if strings.HasPrefix(line, "Name: ") {
			info.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name: "))
		} else if strings.HasPrefix(line, "Version: ") {
			info.Version = strings.TrimSpace(strings.TrimPrefix(line, "Version: "))
		}
	}
	return info, nil
}

// createWrappers creates wrapper scripts for Python package scripts
func (p *PyPiProvider) createWrappers() error {
	// Create wrappers based on nvpm-registry.json bin attribute
	desired := lppPyGetDataForProvider("pypi").Packages
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
		for binName, binCmd := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			// Remove any existing wrapper with the same name to avoid conflicts
			if _, err := pipLstat(wrapperPath); err == nil {
				_ = pipRemove(wrapperPath)
			}
			if err := p.createPythonWrapperForCommand(binCmd, wrapperPath); err != nil {
				Logger.Error(fmt.Sprintf("Error creating wrapper for %s: %v", binName, err))
				continue
			}
			if err := pipChmod(wrapperPath, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Error setting executable permissions for %s: %v", binName, err))
			}
		}
	}
	return nil
}

// normalizePyPiBinCommand turns registry bin values like "pypi:yamllint" into the real console script name "yamllint".
func (p *PyPiProvider) normalizePyPiBinCommand(commandToExec string) string {
	commandToExec = strings.TrimSpace(commandToExec)
	if strings.HasPrefix(commandToExec, p.PREFIX) {
		return strings.TrimPrefix(commandToExec, p.PREFIX)
	}
	return commandToExec
}

// createPythonWrapperForCommand creates a wrapper that prepares the environment and executes the given command.
func (p *PyPiProvider) createPythonWrapperForCommand(commandToExec string, wrapperPath string) error {
	sitePackagesDir := p.findSitePackagesDir()
	binDir := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	if sitePackagesDir == "" {
		sitePackagesDir = p.APP_PACKAGES_DIR
	}
	commandToExec = p.normalizePyPiBinCommand(commandToExec)
	if commandToExec == "" {
		return fmt.Errorf("empty command for wrapper %s", wrapperPath)
	}
	wrapperContent := fmt.Sprintf(`#!/bin/sh
# Sets up Python environment for nvpm-installed packages and runs the target command

# Add the nvpm Python packages to PYTHONPATH and PATH (to resolve console scripts)
export PYTHONPATH="%s:$PYTHONPATH"
export PATH="%s:$PATH"

# Execute the command from registry
exec %s "$@"
`, sitePackagesDir, binDir, commandToExec)
	if err := pipWriteFile(wrapperPath, []byte(wrapperContent), 0755); err != nil {
		return err
	}
	return nil
}

// findSitePackagesDir finds the site-packages directory where pip installed the modules.
// It uses the current Python version to locate the correct directory, ensuring compatibility
// with the latest Python version instead of relying on old versions.
func (p *PyPiProvider) findSitePackagesDir() string {
	// First, try to detect the current Python version and use that
	pythonVersion, err := p.getPythonVersion()
	if err == nil {
		// Construct the expected path using current Python version
		expectedPath := filepath.Join(p.APP_PACKAGES_DIR, "lib", "python"+pythonVersion, "site-packages")
		if _, err := pipStat(expectedPath); err == nil {
			return expectedPath
		}
		// If the directory doesn't exist yet, return the expected path anyway
		// It will be created when packages are installed
		return expectedPath
	}

	// Fallback: search for any python* directory (for backward compatibility)
	libDir := filepath.Join(p.APP_PACKAGES_DIR, "lib")
	if _, err := pipStat(libDir); os.IsNotExist(err) {
		return ""
	}
	entries, err := pipReadDir(libDir)
	if err != nil {
		return ""
	}
	// Prefer newer Python versions by sorting and taking the last one
	var pythonDirs []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "python") {
			pythonDirs = append(pythonDirs, entry.Name())
		}
	}
	if len(pythonDirs) == 0 {
		return ""
	}
	// Sort to prefer newer versions (python3.12 > python3.11 > python3.10)
	// Simple string sort works because "python3.12" > "python3.11" lexicographically
	for i := len(pythonDirs) - 1; i >= 0; i-- {
		sitePackagesPath := filepath.Join(libDir, pythonDirs[i], "site-packages")
		if _, err := pipStat(sitePackagesPath); err == nil {
			return sitePackagesPath
		}
	}
	return ""
}

// removeAllWrappers removes all wrapper scripts from the nvpm bin directory
func (p *PyPiProvider) removeAllWrappers() error {
	// Remove only wrappers managed by the PyPI provider based on registry bin names
	desired := lppPyGetDataForProvider("pypi").Packages
	if len(desired) == 0 {
		return nil
	}
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()
	for _, pkg := range desired {
		registryItem := parser.GetBySourceId(pkg.SourceID)
		for binName := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := pipLstat(wrapperPath); err == nil {
				if err := pipRemove(wrapperPath); err != nil {
					Logger.Error(fmt.Sprintf("Warning: failed to remove wrapper script %s: %v", wrapperPath, err))
				}
			}
		}
	}
	return nil
}

// removePackageWrappers removes wrapper scripts for a specific package
func (p *PyPiProvider) removePackageWrappers(packageName string) error {
	nvpmBinDir := files.GetAppBinPath()
	// Reconstruct sourceId to query registry (use new format)
	sourceID := p.PREFIX + packageName
	parser := registry_parser.NewDefaultRegistryParser()
	registryItem := parser.GetBySourceId(sourceID)
	if len(registryItem.Bin) == 0 {
		return nil
	}
	for binName := range registryItem.Bin {
		wrapperPath := filepath.Join(nvpmBinDir, binName)
		if _, err := pipLstat(wrapperPath); err == nil {
			if err := pipRemove(wrapperPath); err != nil {
				Logger.Error(fmt.Sprintf("Warning: failed to remove wrapper script %s: %v", wrapperPath, err))
			}
		}
	}
	return nil
}

// normalizeDistributionName normalizes a distribution name per PEP 503
func normalizeDistributionName(name string) string {
	n := strings.ToLower(name)
	n = strings.ReplaceAll(n, "_", "-")
	return n
}

// findPackageInfoDir tries to locate the <dist>-<version>.dist-info (or .egg-info) directory for a package
func (p *PyPiProvider) findPackageInfoDir(packageName string) string {
	sitePackagesDir := p.findSitePackagesDir()
	if sitePackagesDir == "" {
		return ""
	}
	normalized := normalizeDistributionName(packageName)
	entries, err := pipReadDir(sitePackagesDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !(strings.HasSuffix(name, ".dist-info") || strings.HasSuffix(name, ".egg-info")) {
			continue
		}
		// Trim suffix and compare the leading portion with normalized package name
		base := strings.TrimSuffix(strings.TrimSuffix(name, ".dist-info"), ".egg-info")
		baseNorm := normalizeDistributionName(base)
		if strings.HasPrefix(baseNorm, normalized+"-") || baseNorm == normalized {
			return filepath.Join(sitePackagesDir, name)
		}
	}
	return ""
}

// parseEntryPointsFromInfoDir parses entry_points.txt to get executable names (console_scripts/gui_scripts)
func (p *PyPiProvider) parseEntryPointsFromInfoDir(infoDir string) []string {
	epPath := filepath.Join(infoDir, "entry_points.txt")
	data, err := pipReadFile(epPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var result []string
	currentSection := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		if currentSection == "console_scripts" || currentSection == "gui_scripts" {
			if idx := strings.Index(line, "="); idx != -1 {
				name := strings.TrimSpace(line[:idx])
				if name != "" {
					result = append(result, name)
				}
			}
		}
	}
	return result
}

func (p *PyPiProvider) Clean() bool {
	_ = p.removeAllWrappers()
	if err := pipRemoveAll(p.APP_PACKAGES_DIR); err != nil {
		Logger.Error("Error removing directory:")
		return false
	}
	return p.Sync()
}

func (p *PyPiProvider) Sync() bool {
	if _, err := pipStat(p.APP_PACKAGES_DIR); os.IsNotExist(err) {
		if err := pipMkdir(p.APP_PACKAGES_DIR, 0755); err != nil {
			fmt.Println("Error creating directory:", err)
			return false
		}
	}

	packagesFound := p.generateRequirementsTxt()
	if !packagesFound {
		return true
	}

	Logger.Info("PyPI Sync: Starting sync process")

	// Ensure we're using the current Python version by detecting it
	pythonVersion, err := p.getPythonVersion()
	if err != nil {
		Logger.Error(fmt.Sprintf("PyPI Sync: Failed to detect Python version: %v", err))
		// Continue anyway, pip will use whatever Python it's associated with
	} else {
		Logger.Info(fmt.Sprintf("PyPI Sync: Using Python version %s", pythonVersion))
	}

	desired := local_packages_parser.GetDataForProvider("pypi").Packages

	if p.areAllPackagesInstalled(desired) {
		Logger.Info("PyPI Sync: All packages already installed correctly, skipping installation")
		_ = p.createWrappers()
		return true
	}

	installed := p.getInstalledPackages()
	allOk := true
	installedCount := 0
	skippedCount := 0

	for _, pkg := range desired {
		name := p.getRepo(pkg.SourceID)
		if v, ok := installed[name]; !ok || v != pkg.Version {
			pkgString := fmt.Sprintf("%s==%s", name, pkg.Version)
			Logger.Info(fmt.Sprintf("PyPI Sync: Installing package %s", pkgString))
			// Use the current pip command which should be associated with the current Python version
			installCode, err := pipShellOut(pipCmd, []string{"install", pkgString, "--prefix", p.APP_PACKAGES_DIR}, p.APP_PACKAGES_DIR, nil)
			if err != nil || installCode != 0 {
				Logger.Error(fmt.Sprintf("Error installing %s==%s: %v", name, pkg.Version, err))
				allOk = false
			} else {
				installedCount++
			}
		} else {
			Logger.Info(fmt.Sprintf("PyPI Sync: Package %s==%s already installed, skipping", name, pkg.Version))
			skippedCount++
		}
	}

	if allOk {
		_ = p.createWrappers()
	}

	Logger.Info(fmt.Sprintf("PyPI Sync: Completed - %d packages installed, %d packages skipped", installedCount, skippedCount))
	return allOk
}

// areAllPackagesInstalled checks if all desired packages are already installed with correct versions
func (p *PyPiProvider) areAllPackagesInstalled(desired []local_packages_parser.LocalPackageItem) bool {
	installed := p.getInstalledPackages()
	for _, pkg := range desired {
		name := p.getRepo(pkg.SourceID)
		if v, ok := installed[name]; !ok || v != pkg.Version {
			return false
		}
	}
	return true
}

// getInstalledPackages gets the list of installed packages using pip freeze scoped to this provider's site-packages
// (--prefix installs are not visible to a plain `pip freeze`, which lists the active interpreter's environment).
func (p *PyPiProvider) getInstalledPackages() map[string]string {
	installed := map[string]string{}
	siteDir := p.findSitePackagesDir()
	args := []string{"freeze"}
	if siteDir != "" {
		args = append(args, "--path", siteDir)
	}
	freezeCode, freezeOut, freezeErr := pipShellOutCapture(pipCmd, args, p.APP_PACKAGES_DIR, nil)
	trimmed := strings.TrimSpace(freezeOut)
	if freezeCode == 0 {
		if trimmed == "" {
			return installed
		}
		lines := strings.Split(freezeOut, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "==")
			if len(parts) == 2 {
				installed[parts[0]] = parts[1]
			}
		}
		return installed
	}
	errMsg := ""
	if freezeErr != nil {
		errMsg = freezeErr.Error()
	}
	Logger.Error(fmt.Sprintf("Error getting installed packages with %s freeze (args=%v): %s; output: %q", pipCmd, args, errMsg, freezeOut))
	return installed
}

func (p *PyPiProvider) Install(sourceID, version string) bool {
	var err error
	if version == "latest" {
		version, err = p.getLatestVersion(p.getRepo(sourceID))
		if err != nil {
			Logger.Error(fmt.Sprintf("Error getting latest version for %s: %v", sourceID, err))
			return false
		}
	}
	if err = lppPyAdd(sourceID, version); err != nil {
		Logger.Error(fmt.Sprintf("Error adding package %s to local packages: %v", sourceID, err))
		return false
	}
	return p.Sync()
}

func (p *PyPiProvider) removeBin(sourceID string) error {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		return fmt.Errorf("invalid source ID format for PyPI provider")
	}
	Logger.Info(fmt.Sprintf("PyPI Remove: Removing bin for package %s", packageName))
	infoDir := p.findPackageInfoDir(packageName)
	if infoDir == "" {
		return fmt.Errorf("no package info directory found for %s", packageName)
	}
	entryPoints := p.parseEntryPointsFromInfoDir(infoDir)
	for _, entryPoint := range entryPoints {
		binPath := filepath.Join(p.APP_PACKAGES_DIR, "bin", entryPoint)
		if _, err := pipLstat(binPath); err == nil {
			if err := pipRemove(binPath); err != nil {
				return fmt.Errorf("failed to remove bin script %s: %v", binPath, err)
			}
		}
	}
	return nil
}

func (p *PyPiProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	Logger.Info(fmt.Sprintf("PyPI Remove: Removing package %s", packageName))
	// Try to remove bin - log error but continue (package might already be partially removed)
	// Only fail if it's a critical error, not just missing directory
	if err := p.removeBin(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Error removing bin for package %s: %v", packageName, err))
		// Continue with wrapper and local package removal even if bin removal fails
		// This handles cases where package is partially installed or already removed
	}
	_ = p.removePackageWrappers(packageName)
	if err := lppPyRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Error removing package %s from local packages: %v", packageName, err))
		return false
	}
	Logger.Info(fmt.Sprintf("PyPI Remove: Package %s removed successfully", packageName))
	return p.Sync()
}

func (p *PyPiProvider) Update(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("Invalid source ID format for PyPI provider")
		return false
	}
	latestVersion, err := p.getLatestVersion(repo)
	if err != nil {
		Logger.Error(fmt.Sprintf("Error getting latest version for %s: %v", repo, err))
		return false
	}
	Logger.Info(fmt.Sprintf("PyPI Update: Updating %s to version %s", repo, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *PyPiProvider) getLatestVersion(packageName string) (string, error) {
	_, output, err := pipShellOutCapture(pipCmd, []string{"index", "versions", packageName}, "", nil)
	if err != nil {
		Logger.Error(fmt.Sprintf("PyPI getLatestVersion: Command failed for %s: %v, output: %s", packageName, err, output))
		return "", err
	}

	// Parse the output format: "Available versions: 25.1.0, 24.10.0, 24.8.0, ..."
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Available versions:") {
			versionsPart := strings.TrimSpace(strings.TrimPrefix(line, "Available versions:"))
			versions := strings.Split(versionsPart, ", ")
			if len(versions) > 0 {
				// Return the first (latest) version
				return strings.TrimSpace(versions[0]), nil
			}
		}
	}

	return "", fmt.Errorf("could not parse versions from pip output: %s", output)
}
