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

type GemProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

var gemCmd = "gem"

// Injectable shell and OS helpers for tests
var gemShellOut = shell_out.ShellOut
var gemShellOutCapture = shell_out.ShellOutCapture
var gemHasCommand = shell_out.HasCommand
var gemCreate = os.Create
var gemReadDir = os.ReadDir
var gemLstat = os.Lstat
var gemRemove = os.Remove
var gemChmod = os.Chmod
var gemStat = os.Stat
var gemMkdirAll = os.MkdirAll
var gemRemoveAll = os.RemoveAll
var gemWriteFile = os.WriteFile
var gemClose = func(f *os.File) error { return f.Close() }

// Injectable local packages helpers for tests
var lppGemAdd = local_packages_parser.AddLocalPackage
var lppGemRemove = local_packages_parser.RemoveLocalPackage
var lppGemGetDataForProvider = local_packages_parser.GetDataForProvider
var lppGemGetData = local_packages_parser.GetData

func NewProviderGem() *GemProvider {
	p := &GemProvider{}
	p.PROVIDER_NAME = "gem"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	// gemCmd defaults to "gem"
	return p
}

func (p *GemProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:gem/pkg) and new (gem:pkg) formats
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

func (p *GemProvider) packageDir(gemName string) string {
	return filepath.Join(p.APP_PACKAGES_DIR, gemName)
}

func (p *GemProvider) generateGemfile() bool {
	found := false
	localPackages := lppGemGetData(true).Packages
	for _, pkg := range localPackages {
		if detectProvider(pkg.SourceID) != ProviderGem {
			continue
		}
		gemName := p.getRepo(pkg.SourceID)
		if gemName == "" {
			continue
		}
		dir := p.packageDir(gemName)
		if err := gemMkdirAll(dir, 0755); err != nil {
			Logger.Error(fmt.Sprintf("Error creating directory %s: %s", dir, err))
			return false
		}
		line := fmt.Sprintf("gem '%s'\n", gemName)
		if pkg.Version != "" && pkg.Version != "latest" {
			line = fmt.Sprintf("gem '%s', '~> %s'\n", gemName, pkg.Version)
		}
		filePath := filepath.Join(dir, "Gemfile")
		file, err := gemCreate(filePath)
		if err != nil {
			Logger.Error(fmt.Sprintf("Error creating Gemfile: %s", err))
			return false
		}
		if _, err := file.WriteString(line); err != nil {
			_ = gemClose(file)
			Logger.Error(fmt.Sprintf("Error writing to Gemfile: %s", err))
			return false
		}
		if closeErr := gemClose(file); closeErr != nil {
			_ = fmt.Errorf("warning: failed to close Gemfile: %v", closeErr)
		}
		found = true
	}
	return found
}

func (p *GemProvider) Install(sourceID, version string) bool {
	gemName := p.getRepo(sourceID)
	if gemName == "" {
		Logger.Error("Gem Install: Invalid source ID format")
		return false
	}

	if !gemHasCommand("gem", []string{"--version"}, nil) {
		Logger.Error("Gem Install: gem command not found. Please install Ruby and RubyGems.")
		return false
	}

	dir := p.packageDir(gemName)
	if err := gemMkdirAll(dir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("Gem Install: Error creating packages directory: %v", err))
		return false
	}

	args := []string{"install", gemName, "--install-dir", dir, "--no-document", "--no-user-install"}
	if version != "" && version != "latest" {
		args = append(args, "--version", version)
	}

	Logger.Info(fmt.Sprintf("Gem Install: Installing %s@%s", gemName, version))
	code, err := gemShellOut(gemCmd, args, "", nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("Gem Install: Error installing gem: %v", err))
		return false
	}

	// Get installed version
	installedVersion := version
	if installedVersion == "" || installedVersion == "latest" {
		// Try to get the installed version
		code, output, err := gemShellOutCapture(gemCmd, []string{"list", gemName, "--install-dir", dir}, "", nil)
		if err == nil && code == 0 {
			// Parse output like "gemname (1.2.3)"
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				if strings.Contains(line, gemName) {
					// Extract version from line like "gemname (1.2.3)"
					re := regexp.MustCompile(`\(([^)]+)\)`)
					matches := re.FindStringSubmatch(line)
					if len(matches) > 1 {
						installedVersion = matches[1]
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
	if err := lppGemAdd(sourceID, installedVersion); err != nil {
		Logger.Error(fmt.Sprintf("Gem Install: Error adding package to local packages: %v", err))
		return false
	}

	// Generate Gemfile
	_ = p.generateGemfile()

	// Create wrappers for binaries
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("Gem Install: Warning creating wrappers: %v", err))
		// Don't fail installation if wrappers fail
	}
	p.cleanupLegacyGemRoot()

	Logger.Info(fmt.Sprintf("Gem Install: Successfully installed %s@%s", gemName, installedVersion))
	return true
}

func (p *GemProvider) Remove(sourceID string) bool {
	gemName := p.getRepo(sourceID)
	if gemName == "" {
		Logger.Error("Gem Remove: Invalid source ID format")
		return false
	}

	if !gemHasCommand("gem", []string{"--version"}, nil) {
		Logger.Error("Gem Remove: gem command not found. Please install Ruby and RubyGems.")
		return false
	}

	Logger.Info(fmt.Sprintf("Gem Remove: Removing %s", gemName))

	// Remove wrappers
	if err := p.removeWrappersForGem(gemName); err != nil {
		Logger.Info(fmt.Sprintf("Gem Remove: Warning removing wrappers: %v", err))
	}

	// Uninstall gem
	args := []string{"uninstall", gemName, "--install-dir", p.packageDir(gemName), "--executables", "--ignore-dependencies"}
	code, err := gemShellOut(gemCmd, args, "", nil)
	if err != nil || code != 0 {
		Logger.Info(fmt.Sprintf("Gem Remove: Warning uninstalling gem (may not be installed): %v", err))
		// Don't fail if gem is not installed
	}

	// Remove from local packages
	if err := lppGemRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Gem Remove: Error removing package from local packages: %v", err))
		return false
	}
	if err := gemRemoveAll(p.packageDir(gemName)); err != nil {
		Logger.Info(fmt.Sprintf("Gem Remove: Warning removing package directory: %v", err))
	}

	// Regenerate Gemfile
	_ = p.generateGemfile()

	Logger.Info(fmt.Sprintf("Gem Remove: Successfully removed %s", gemName))
	return true
}

func (p *GemProvider) Update(sourceID string) bool {
	gemName := p.getRepo(sourceID)
	if gemName == "" {
		Logger.Error("Gem Update: Invalid source ID format")
		return false
	}

	if !gemHasCommand("gem", []string{"--version"}, nil) {
		Logger.Error("Gem Update: gem command not found. Please install Ruby and RubyGems.")
		return false
	}

	Logger.Info(fmt.Sprintf("Gem Update: Updating %s", gemName))

	// Update gem to latest version
	dir := p.packageDir(gemName)
	args := []string{"update", gemName, "--install-dir", dir, "--no-document", "--no-user-install"}
	code, err := gemShellOut(gemCmd, args, "", nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("Gem Update: Error updating gem: %v", err))
		return false
	}

	// Get updated version
	code, output, err := gemShellOutCapture(gemCmd, []string{"list", gemName, "--install-dir", p.packageDir(gemName)}, "", nil)
	var updatedVersion string
	if err == nil && code == 0 {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, gemName) {
				re := regexp.MustCompile(`\(([^)]+)\)`)
				matches := re.FindStringSubmatch(line)
				if len(matches) > 1 {
					updatedVersion = matches[1]
					break
				}
			}
		}
	}
	if updatedVersion == "" {
		updatedVersion = "latest"
	}

	// Update local packages
	if err := lppGemRemove(sourceID); err == nil {
		if err := lppGemAdd(sourceID, updatedVersion); err != nil {
			Logger.Error(fmt.Sprintf("Gem Update: Error updating package in local packages: %v", err))
			return false
		}
	}

	// Regenerate Gemfile
	_ = p.generateGemfile()

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("Gem Update: Warning recreating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("Gem Update: Successfully updated %s@%s", gemName, updatedVersion))
	return true
}

func (p *GemProvider) getLatestVersion(packageName string) (string, error) {
	if !gemHasCommand("gem", []string{"--version"}, nil) {
		return "", fmt.Errorf("gem command not found")
	}

	code, output, err := gemShellOutCapture(gemCmd, []string{"search", "-r", packageName, "--remote"}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("failed to search for gem: %v", err)
	}

	// Parse output to find version
	// Output format: "gemname (1.2.3)"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, packageName) {
			re := regexp.MustCompile(`\(([^)]+)\)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				return matches[1], nil
			}
		}
	}

	return "", fmt.Errorf("version not found")
}

// findGemBinDir finds the gem bin directory
func (p *GemProvider) findGemBinDir() string {
	return p.findGemBinDirIn(p.APP_PACKAGES_DIR)
}

func (p *GemProvider) findGemBinDirIn(prefixDir string) string {
	return filepath.Join(prefixDir, "bin")
}

// createWrappers creates wrapper scripts for gem executables
func (p *GemProvider) createWrappers() error {
	// Create wrappers based on nvpm-registry.json bin attribute
	desired := lppGemGetDataForProvider("gem").Packages
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
			// Remove any existing wrapper with the same name to avoid conflicts
			if _, err := gemLstat(wrapperPath); err == nil {
				_ = gemRemove(wrapperPath)
			}
			if err := p.createGemWrapperForPrefix(binCmd, wrapperPath, prefixDir); err != nil {
				Logger.Error(fmt.Sprintf("Error creating wrapper for %s: %v", binName, err))
				continue
			}
			if err := gemChmod(wrapperPath, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Error setting executable permissions for %s: %v", binName, err))
			}
		}
	}
	return nil
}

// createGemWrapperForCommand creates a wrapper that prepares the environment and executes the given command
func (p *GemProvider) createGemWrapperForCommand(commandToExec string, wrapperPath string) error {
	return p.createGemWrapperForPrefix(commandToExec, wrapperPath, p.APP_PACKAGES_DIR)
}

func (p *GemProvider) createGemWrapperForPrefix(commandToExec string, wrapperPath string, prefixDir string) error {
	if prefixDir == "" {
		prefixDir = p.APP_PACKAGES_DIR
	}
	gemBinDir := p.findGemBinDirIn(prefixDir)
	if commandToExec == "" {
		return fmt.Errorf("empty command for wrapper %s", wrapperPath)
	}

	var execCmd string
	if strings.HasPrefix(commandToExec, "ruby:") {
		execPath := strings.TrimPrefix(commandToExec, "ruby:")
		gemLibDir := filepath.Join(prefixDir, "gems")
		execCmd = p.findGemExecutable(gemLibDir, execPath)
		if execCmd == "" {
			execCmd = filepath.Join(prefixDir, execPath)
		}
	} else {
		execCmd = filepath.Join(gemBinDir, commandToExec)
	}

	wrapperContent := fmt.Sprintf(`#!/bin/sh
# Sets up Ruby/Gem environment for nvpm-installed packages and runs the target command

export GEM_HOME="%s"
export GEM_PATH="%s"
export PATH="%s:$PATH"
export RUBYLIB="%s:$RUBYLIB"

exec %s "$@"
`, prefixDir, prefixDir, gemBinDir, prefixDir, execCmd)

	if err := gemWriteFile(wrapperPath, []byte(wrapperContent), 0755); err != nil {
		return err
	}
	return nil
}

// findGemExecutable searches for an executable in gem directories
func (p *GemProvider) findGemExecutable(gemLibDir, execPath string) string {
	// Look for the executable in installed gem directories
	if entries, err := gemReadDir(gemLibDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				gemDir := filepath.Join(gemLibDir, entry.Name())
				execFile := filepath.Join(gemDir, execPath)
				if _, err := gemStat(execFile); err == nil {
					return execFile
				}
			}
		}
	}
	return ""
}

// removeWrappersForGem removes wrapper scripts for a specific gem
func (p *GemProvider) removeWrappersForGem(gemName string) error {
	desired := lppGemGetDataForProvider("gem").Packages
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()

	// Find packages that match this gem
	for _, pkg := range desired {
		if p.getRepo(pkg.SourceID) != gemName {
			continue
		}
		registryItem := parser.GetBySourceId(pkg.SourceID)
		for binName := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := gemLstat(wrapperPath); err == nil {
				if err := gemRemove(wrapperPath); err != nil {
					Logger.Info(fmt.Sprintf("Gem: Warning removing wrapper %s: %v", wrapperPath, err))
				}
			}
		}
	}
	return nil
}

func (p *GemProvider) isGemInstalled(gemName, version string) bool {
	specDir := filepath.Join(p.packageDir(gemName), "specifications")
	entries, err := gemReadDir(specDir)
	if err != nil {
		return false
	}
	prefix := gemName + "-"
	if version != "" && version != "latest" {
		prefix = gemName + "-" + version
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".gemspec") || !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".gemspec")
		if rest == "" || strings.HasPrefix(rest, "-") {
			return true
		}
	}
	return false
}

func (p *GemProvider) cleanupLegacyGemRoot() {
	rootGemfile := filepath.Join(p.APP_PACKAGES_DIR, "Gemfile")
	if _, err := gemStat(rootGemfile); err == nil {
		_ = gemRemove(rootGemfile)
	}
	rootLock := filepath.Join(p.APP_PACKAGES_DIR, "Gemfile.lock")
	if _, err := gemStat(rootLock); err == nil {
		_ = gemRemove(rootLock)
	}
	for _, name := range []string{"bin", "gems", "specifications", "cache", "extensions", "doc", "bundler"} {
		path := filepath.Join(p.APP_PACKAGES_DIR, name)
		if _, err := gemStat(path); err == nil {
			_ = gemRemoveAll(path)
		}
	}
}

func (p *GemProvider) Sync() bool {
	Logger.Info("Gem Sync: Syncing gem packages")
	localPackages := lppGemGetDataForProvider(p.PROVIDER_NAME).Packages

	if len(localPackages) == 0 {
		p.cleanupLegacyGemRoot()
		return true
	}

	if !gemHasCommand("gem", []string{"--version"}, nil) {
		Logger.Error("Gem Sync: gem command not found. Please install Ruby and RubyGems.")
		return false
	}

	allOk := true
	for _, pkg := range localPackages {
		gemName := p.getRepo(pkg.SourceID)
		if gemName == "" {
			continue
		}
		dir := p.packageDir(gemName)
		if err := gemMkdirAll(dir, 0755); err != nil {
			Logger.Error(fmt.Sprintf("Gem Sync: Error creating directory %s: %v", dir, err))
			allOk = false
			continue
		}
		if p.isGemInstalled(gemName, pkg.Version) {
			continue
		}
		args := []string{"install", gemName, "--install-dir", dir, "--no-document", "--no-user-install"}
		if pkg.Version != "" && pkg.Version != "latest" {
			args = append(args, "--version", pkg.Version)
		}
		code, err := gemShellOut(gemCmd, args, "", nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("Gem Sync: Error installing %s: %v", gemName, err))
			allOk = false
		}
	}

	_ = p.generateGemfile()
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("Gem Sync: Warning creating wrappers: %v", err))
	}
	p.cleanupLegacyGemRoot()
	return allOk
}
