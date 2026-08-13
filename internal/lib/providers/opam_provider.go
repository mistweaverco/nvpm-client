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

type OpamProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

var opamCmd = "opam"

// Injectable shell and OS helpers for tests
var opamShellOut = shell_out.ShellOut
var opamShellOutCapture = shell_out.ShellOutCapture
var opamHasCommand = shell_out.HasCommand
var opamLstat = os.Lstat
var opamRemove = os.Remove
var opamChmod = os.Chmod
var opamStat = os.Stat
var opamMkdirAll = os.MkdirAll
var opamRemoveAll = os.RemoveAll
var opamWriteFile = os.WriteFile

// Injectable local packages helpers for tests
var lppOpamAdd = local_packages_parser.AddLocalPackage
var lppOpamRemove = local_packages_parser.RemoveLocalPackage
var lppOpamGetDataForProvider = local_packages_parser.GetDataForProvider

func NewProviderOpam() *OpamProvider {
	p := &OpamProvider{}
	p.PROVIDER_NAME = "opam"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *OpamProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:opam/pkg) and new (opam:pkg) formats
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

func (p *OpamProvider) packageDir(packageName string) string {
	return filepath.Join(p.APP_PACKAGES_DIR, packageName)
}

func (p *OpamProvider) switchPath(packageName string) string {
	return filepath.Join(p.packageDir(packageName), "switch")
}

func (p *OpamProvider) ensureSwitch(switchPath string) bool {
	if _, err := opamStat(switchPath); err == nil {
		return true
	}
	code, err := opamShellOut(opamCmd, []string{"switch", "create", switchPath, "ocaml-base-compiler.5.1.0", "--no-switch"}, "", nil)
	if err != nil || code != 0 {
		code, err = opamShellOut(opamCmd, []string{"switch", "create", switchPath, "--no-switch"}, "", nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("OPAM: Error creating switch: %v", err))
			return false
		}
	}
	return true
}

func (p *OpamProvider) Install(sourceID, version string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("OPAM Install: Invalid source ID format")
		return false
	}

	if !opamHasCommand("opam", []string{"--version"}, nil) {
		Logger.Error("OPAM Install: opam command not found. Please install OPAM.")
		return false
	}

	dir := p.packageDir(packageName)
	if err := opamMkdirAll(dir, 0755); err != nil {
		Logger.Error(fmt.Sprintf("OPAM Install: Error creating packages directory: %v", err))
		return false
	}

	switchPath := p.switchPath(packageName)
	if !p.ensureSwitch(switchPath) {
		return false
	}

	// Build opam install command
	packageSpec := packageName
	if version != "" && version != "latest" {
		packageSpec = fmt.Sprintf("%s.%s", packageName, version)
	}

	Logger.Info(fmt.Sprintf("OPAM Install: Installing %s@%s", packageName, version))
	code, err := opamShellOut(opamCmd, []string{"install", packageSpec, "--switch", switchPath, "--yes", "--no-depexts"}, "", nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("OPAM Install: Error installing package: %v", err))
		return false
	}

	// Get installed version
	installedVersion := version
	if installedVersion == "" || installedVersion == "latest" {
		// Try to get the installed version
		code, output, err := opamShellOutCapture(opamCmd, []string{"list", "--switch", switchPath, "--installed"}, "", nil)
		if err == nil && code == 0 {
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				if strings.Contains(line, packageName) {
					// Parse output like "package-name   1.2.3"
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						installedVersion = parts[1]
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
	if err := lppOpamAdd(sourceID, installedVersion); err != nil {
		Logger.Error(fmt.Sprintf("OPAM Install: Error adding package to local packages: %v", err))
		return false
	}

	// Create wrappers for binaries
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("OPAM Install: Warning creating wrappers: %v", err))
	}
	p.cleanupLegacyOpamRoot()

	Logger.Info(fmt.Sprintf("OPAM Install: Successfully installed %s@%s", packageName, installedVersion))
	return true
}

func (p *OpamProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("OPAM Remove: Invalid source ID format")
		return false
	}

	if !opamHasCommand("opam", []string{"--version"}, nil) {
		Logger.Error("OPAM Remove: opam command not found. Please install OPAM.")
		return false
	}

	Logger.Info(fmt.Sprintf("OPAM Remove: Removing %s", packageName))

	switchPath := p.switchPath(packageName)

	// Remove wrappers
	if err := p.removeWrappersForPackage(packageName); err != nil {
		Logger.Info(fmt.Sprintf("OPAM Remove: Warning removing wrappers: %v", err))
	}

	// Uninstall package
	code, err := opamShellOut(opamCmd, []string{"remove", packageName, "--switch", switchPath, "--yes"}, "", nil)
	if err != nil || code != 0 {
		Logger.Info(fmt.Sprintf("OPAM Remove: Warning uninstalling package (may not be installed): %v", err))
	}

	// Remove from local packages
	if err := lppOpamRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("OPAM Remove: Error removing package from local packages: %v", err))
		return false
	}
	if err := opamRemoveAll(p.packageDir(packageName)); err != nil {
		Logger.Info(fmt.Sprintf("OPAM Remove: Warning removing package directory: %v", err))
	}

	Logger.Info(fmt.Sprintf("OPAM Remove: Successfully removed %s", packageName))
	return true
}

func (p *OpamProvider) Update(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("OPAM Update: Invalid source ID format")
		return false
	}

	if !opamHasCommand("opam", []string{"--version"}, nil) {
		Logger.Error("OPAM Update: opam command not found. Please install OPAM.")
		return false
	}

	Logger.Info(fmt.Sprintf("OPAM Update: Updating %s", packageName))

	switchPath := p.switchPath(packageName)

	// Update package
	code, err := opamShellOut(opamCmd, []string{"upgrade", packageName, "--switch", switchPath, "--yes", "--no-depexts"}, "", nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("OPAM Update: Error updating package: %v", err))
		return false
	}

	// Get updated version
	var updatedVersion string
	code, output, err := opamShellOutCapture(opamCmd, []string{"list", "--switch", switchPath, "--installed"}, "", nil)
	if err == nil && code == 0 {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, packageName) {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					updatedVersion = parts[1]
					break
				}
			}
		}
	}
	if updatedVersion == "" {
		updatedVersion = "latest"
	}

	// Update local packages
	if err := lppOpamRemove(sourceID); err == nil {
		if err := lppOpamAdd(sourceID, updatedVersion); err != nil {
			Logger.Error(fmt.Sprintf("OPAM Update: Error updating package in local packages: %v", err))
			return false
		}
	}

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("OPAM Update: Warning recreating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("OPAM Update: Successfully updated %s@%s", packageName, updatedVersion))
	return true
}

func (p *OpamProvider) getLatestVersion(packageName string) (string, error) {
	if !opamHasCommand("opam", []string{"--version"}, nil) {
		return "", fmt.Errorf("opam command not found")
	}

	code, output, err := opamShellOutCapture(opamCmd, []string{"show", packageName, "--field", "version"}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("failed to get package info: %v", err)
	}

	version := strings.TrimSpace(output)
	if version == "" {
		return "", fmt.Errorf("version not found")
	}

	return version, nil
}

func (p *OpamProvider) findOpamBinDir() string {
	return p.findOpamBinDirIn(filepath.Join(p.APP_PACKAGES_DIR, "switch"))
}

func (p *OpamProvider) findOpamBinDirIn(switchPath string) string {
	return filepath.Join(switchPath, "bin")
}

func (p *OpamProvider) isOpamPackageInstalled(packageName string) bool {
	binDir := p.findOpamBinDirIn(p.switchPath(packageName))
	_, err := opamStat(binDir)
	return err == nil
}

// createWrappers creates wrapper scripts for OPAM executables
func (p *OpamProvider) createWrappers() error {
	desired := lppOpamGetDataForProvider("opam").Packages
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
		switchPath := p.switchPath(p.getRepo(pkg.SourceID))
		for binName, binCmd := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := opamLstat(wrapperPath); err == nil {
				_ = opamRemove(wrapperPath)
			}
			if err := p.createOpamWrapperForPrefix(binCmd, wrapperPath, switchPath); err != nil {
				Logger.Error(fmt.Sprintf("Error creating wrapper for %s: %v", binName, err))
				continue
			}
			if err := opamChmod(wrapperPath, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Error setting executable permissions for %s: %v", binName, err))
			}
		}
	}
	return nil
}

// createOpamWrapperForCommand creates a wrapper that prepares the environment and executes the given command
func (p *OpamProvider) createOpamWrapperForCommand(commandToExec string, wrapperPath string) error {
	return p.createOpamWrapperForPrefix(commandToExec, wrapperPath, filepath.Join(p.APP_PACKAGES_DIR, "switch"))
}

func (p *OpamProvider) createOpamWrapperForPrefix(commandToExec string, wrapperPath string, switchPath string) error {
	if switchPath == "" {
		switchPath = filepath.Join(p.APP_PACKAGES_DIR, "switch")
	}
	opamBinDir := p.findOpamBinDirIn(switchPath)
	if commandToExec == "" {
		return fmt.Errorf("empty command for wrapper %s", wrapperPath)
	}

	// The command might be a path like "opam:binary-name" or just "binary-name"
	var execCmd string
	if strings.HasPrefix(commandToExec, "opam:") {
		binName := strings.TrimPrefix(commandToExec, "opam:")
		execCmd = filepath.Join(opamBinDir, binName)
	} else {
		execCmd = filepath.Join(opamBinDir, commandToExec)
	}

	wrapperContent := fmt.Sprintf(`#!/bin/sh
# Sets up OCaml/OPAM environment for nvpm-installed packages and runs the target command

# Add the nvpm OPAM bin directory to PATH
export PATH="%s:$PATH"

# Set OPAM switch
export OPAM_SWITCH_PREFIX="%s"

# Execute the command from registry
exec %s "$@"
`, opamBinDir, switchPath, execCmd)

	if err := opamWriteFile(wrapperPath, []byte(wrapperContent), 0755); err != nil {
		return err
	}
	return nil
}

// removeWrappersForPackage removes wrapper scripts for a specific package
func (p *OpamProvider) removeWrappersForPackage(packageName string) error {
	desired := lppOpamGetDataForProvider("opam").Packages
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()

	for _, pkg := range desired {
		if p.getRepo(pkg.SourceID) != packageName {
			continue
		}
		registryItem := parser.GetBySourceId(pkg.SourceID)
		for binName := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := opamLstat(wrapperPath); err == nil {
				if err := opamRemove(wrapperPath); err != nil {
					Logger.Info(fmt.Sprintf("OPAM: Warning removing wrapper %s: %v", wrapperPath, err))
				}
			}
		}
	}
	return nil
}

func (p *OpamProvider) cleanupLegacyOpamRoot() {
	legacySwitch := filepath.Join(p.APP_PACKAGES_DIR, "switch")
	legacyBin := filepath.Join(legacySwitch, "bin")
	nestedSwitch := filepath.Join(legacySwitch, "switch")
	if _, err := opamStat(legacyBin); err != nil {
		return
	}
	if _, err := opamStat(nestedSwitch); err == nil {
		return
	}
	_ = opamRemoveAll(legacySwitch)
}

func (p *OpamProvider) Sync() bool {
	Logger.Info("OPAM Sync: Syncing OPAM packages")
	localPackages := lppOpamGetDataForProvider(p.PROVIDER_NAME).Packages

	if len(localPackages) == 0 {
		p.cleanupLegacyOpamRoot()
		return true
	}

	if !opamHasCommand("opam", []string{"--version"}, nil) {
		Logger.Error("OPAM Sync: opam command not found. Please install OPAM.")
		return false
	}

	allOk := true
	for _, pkg := range localPackages {
		packageName := p.getRepo(pkg.SourceID)
		if packageName == "" {
			continue
		}
		dir := p.packageDir(packageName)
		if err := opamMkdirAll(dir, 0755); err != nil {
			Logger.Error(fmt.Sprintf("OPAM Sync: Error creating directory %s: %v", dir, err))
			allOk = false
			continue
		}
		if p.isOpamPackageInstalled(packageName) {
			continue
		}
		switchPath := p.switchPath(packageName)
		if !p.ensureSwitch(switchPath) {
			allOk = false
			continue
		}
		packageSpec := packageName
		if pkg.Version != "" && pkg.Version != "latest" {
			packageSpec = fmt.Sprintf("%s.%s", packageName, pkg.Version)
		}
		code, err := opamShellOut(opamCmd, []string{"install", packageSpec, "--switch", switchPath, "--yes", "--no-depexts"}, "", nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("OPAM Sync: Error installing %s: %v", packageName, err))
			allOk = false
		}
	}

	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("OPAM Sync: Warning creating wrappers: %v", err))
	}
	p.cleanupLegacyOpamRoot()
	return allOk
}
