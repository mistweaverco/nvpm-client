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

type LuaRocksProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

var luarocksCmd = "luarocks"

// Injectable shell and OS helpers for tests
var luarocksShellOut = shell_out.ShellOut
var luarocksShellOutCapture = shell_out.ShellOutCapture
var luarocksHasCommand = shell_out.HasCommand
var luarocksLstat = os.Lstat
var luarocksRemove = os.Remove
var luarocksChmod = os.Chmod
var luarocksStat = os.Stat
var luarocksMkdirAll = os.MkdirAll
var luarocksWriteFile = os.WriteFile

// Injectable local packages helpers for tests
var lppLuarocksAdd = local_packages_parser.AddLocalPackage
var lppLuarocksRemove = local_packages_parser.RemoveLocalPackage
var lppLuarocksGetDataForProvider = local_packages_parser.GetDataForProvider

func NewProviderLuaRocks() *LuaRocksProvider {
	p := &LuaRocksProvider{}
	p.PROVIDER_NAME = "luarocks"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *LuaRocksProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:luarocks/pkg) and new (luarocks:pkg) formats
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

func (p *LuaRocksProvider) Install(sourceID, version string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("LuaRocks Install: Invalid source ID format")
		return false
	}

	if !luarocksHasCommand("luarocks", []string{"--version"}, nil) {
		Logger.Error("LuaRocks Install: luarocks command not found. Please install LuaRocks.")
		return false
	}

	// Ensure packages directory exists
	if err := luarocksMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
		Logger.Error(fmt.Sprintf("LuaRocks Install: Error creating packages directory: %v", err))
		return false
	}

	// Build luarocks install command
	packageSpec := packageName
	if version != "" && version != "latest" {
		packageSpec = fmt.Sprintf("%s %s", packageName, version)
	}
	args := []string{"install", packageSpec, "--tree", p.APP_PACKAGES_DIR}

	Logger.Info(fmt.Sprintf("LuaRocks Install: Installing %s@%s", packageName, version))
	code, err := luarocksShellOut(luarocksCmd, args, "", nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("LuaRocks Install: Error installing rock: %v", err))
		return false
	}

	// Get installed version
	installedVersion := version
	if installedVersion == "" || installedVersion == "latest" {
		// Try to get the installed version
		code, output, err := luarocksShellOutCapture(luarocksCmd, []string{"list", "--tree", p.APP_PACKAGES_DIR}, "", nil)
		if err == nil && code == 0 {
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				if strings.Contains(line, packageName) {
					// Parse output like "package-name   1.2.3-1"
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						installedVersion = strings.Split(parts[1], "-")[0] // Remove revision suffix
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
	if err := lppLuarocksAdd(sourceID, installedVersion); err != nil {
		Logger.Error(fmt.Sprintf("LuaRocks Install: Error adding package to local packages: %v", err))
		return false
	}

	// Create wrappers for binaries
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("LuaRocks Install: Warning creating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("LuaRocks Install: Successfully installed %s@%s", packageName, installedVersion))
	return true
}

func (p *LuaRocksProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("LuaRocks Remove: Invalid source ID format")
		return false
	}

	if !luarocksHasCommand("luarocks", []string{"--version"}, nil) {
		Logger.Error("LuaRocks Remove: luarocks command not found. Please install LuaRocks.")
		return false
	}

	Logger.Info(fmt.Sprintf("LuaRocks Remove: Removing %s", packageName))

	// Remove wrappers
	if err := p.removeWrappersForPackage(packageName); err != nil {
		Logger.Info(fmt.Sprintf("LuaRocks Remove: Warning removing wrappers: %v", err))
	}

	// Uninstall rock
	code, err := luarocksShellOut(luarocksCmd, []string{"remove", packageName, "--tree", p.APP_PACKAGES_DIR}, "", nil)
	if err != nil || code != 0 {
		Logger.Info(fmt.Sprintf("LuaRocks Remove: Warning uninstalling rock (may not be installed): %v", err))
	}

	// Remove from local packages
	if err := lppLuarocksRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("LuaRocks Remove: Error removing package from local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("LuaRocks Remove: Successfully removed %s", packageName))
	return true
}

func (p *LuaRocksProvider) Update(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("LuaRocks Update: Invalid source ID format")
		return false
	}

	if !luarocksHasCommand("luarocks", []string{"--version"}, nil) {
		Logger.Error("LuaRocks Update: luarocks command not found. Please install LuaRocks.")
		return false
	}

	Logger.Info(fmt.Sprintf("LuaRocks Update: Updating %s", packageName))

	// Update rock
	code, err := luarocksShellOut(luarocksCmd, []string{"install", packageName, "--tree", p.APP_PACKAGES_DIR, "--force"}, "", nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("LuaRocks Update: Error updating rock: %v", err))
		return false
	}

	// Get updated version
	var updatedVersion string
	code, output, err := luarocksShellOutCapture(luarocksCmd, []string{"list", "--tree", p.APP_PACKAGES_DIR}, "", nil)
	if err == nil && code == 0 {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, packageName) {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					updatedVersion = strings.Split(parts[1], "-")[0]
					break
				}
			}
		}
	}
	if updatedVersion == "" {
		updatedVersion = "latest"
	}

	// Update local packages
	if err := lppLuarocksRemove(sourceID); err == nil {
		if err := lppLuarocksAdd(sourceID, updatedVersion); err != nil {
			Logger.Error(fmt.Sprintf("LuaRocks Update: Error updating package in local packages: %v", err))
			return false
		}
	}

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("LuaRocks Update: Warning recreating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("LuaRocks Update: Successfully updated %s@%s", packageName, updatedVersion))
	return true
}

func (p *LuaRocksProvider) getLatestVersion(packageName string) (string, error) {
	if !luarocksHasCommand("luarocks", []string{"--version"}, nil) {
		return "", fmt.Errorf("luarocks command not found")
	}

	code, output, err := luarocksShellOutCapture(luarocksCmd, []string{"search", packageName}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("failed to search for rock: %v", err)
	}

	// Parse output to find version
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, packageName) {
			// Output format: "package-name   1.2.3-1"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.Split(parts[1], "-")[0], nil
			}
		}
	}

	return "", fmt.Errorf("version not found")
}

// findLuaRocksBinDir finds the LuaRocks bin directory
func (p *LuaRocksProvider) findLuaRocksBinDir() string {
	// LuaRocks installs binaries to: APP_PACKAGES_DIR/bin
	binDir := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	if _, err := luarocksStat(binDir); err == nil {
		return binDir
	}
	return binDir
}

// createWrappers creates wrapper scripts for LuaRocks executables
func (p *LuaRocksProvider) createWrappers() error {
	desired := lppLuarocksGetDataForProvider("luarocks").Packages
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
			if _, err := luarocksLstat(wrapperPath); err == nil {
				_ = luarocksRemove(wrapperPath)
			}
			if err := p.createLuaRocksWrapperForCommand(binCmd, wrapperPath); err != nil {
				Logger.Error(fmt.Sprintf("Error creating wrapper for %s: %v", binName, err))
				continue
			}
			if err := luarocksChmod(wrapperPath, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Error setting executable permissions for %s: %v", binName, err))
			}
		}
	}
	return nil
}

// createLuaRocksWrapperForCommand creates a wrapper that prepares the environment and executes the given command
func (p *LuaRocksProvider) createLuaRocksWrapperForCommand(commandToExec string, wrapperPath string) error {
	luarocksBinDir := p.findLuaRocksBinDir()
	luarocksLibDir := filepath.Join(p.APP_PACKAGES_DIR, "lib", "luarocks", "rocks")
	if commandToExec == "" {
		return fmt.Errorf("empty command for wrapper %s", wrapperPath)
	}

	// The command might be a path like "luarocks:binary-name" or just "binary-name"
	var execCmd string
	if strings.HasPrefix(commandToExec, "luarocks:") {
		binName := strings.TrimPrefix(commandToExec, "luarocks:")
		execCmd = filepath.Join(luarocksBinDir, binName)
	} else {
		execCmd = filepath.Join(luarocksBinDir, commandToExec)
	}

	wrapperContent := fmt.Sprintf(`#!/bin/sh
# Sets up Lua/LuaRocks environment for nvpm-installed packages and runs the target command

# Add the nvpm LuaRocks bin directory to PATH
export PATH="%s:$PATH"

# Add LuaRocks lib directory to LUA_PATH
export LUA_PATH="%s/?.lua;%s/?/init.lua;$LUA_PATH"

# Execute the command from registry
exec %s "$@"
`, luarocksBinDir, luarocksLibDir, luarocksLibDir, execCmd)

	if err := luarocksWriteFile(wrapperPath, []byte(wrapperContent), 0755); err != nil {
		return err
	}
	return nil
}

// removeWrappersForPackage removes wrapper scripts for a specific package
func (p *LuaRocksProvider) removeWrappersForPackage(packageName string) error {
	desired := lppLuarocksGetDataForProvider("luarocks").Packages
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()

	for _, pkg := range desired {
		if p.getRepo(pkg.SourceID) != packageName {
			continue
		}
		registryItem := parser.GetBySourceId(pkg.SourceID)
		for binName := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := luarocksLstat(wrapperPath); err == nil {
				if err := luarocksRemove(wrapperPath); err != nil {
					Logger.Info(fmt.Sprintf("LuaRocks: Warning removing wrapper %s: %v", wrapperPath, err))
				}
			}
		}
	}
	return nil
}

func (p *LuaRocksProvider) Sync() bool {
	Logger.Info("LuaRocks Sync: Syncing LuaRocks packages")
	localPackages := lppLuarocksGetDataForProvider(p.PROVIDER_NAME).Packages

	if len(localPackages) == 0 {
		return true
	}

	// Check for luarocks command before proceeding
	if !luarocksHasCommand("luarocks", []string{"--version"}, nil) {
		Logger.Error("LuaRocks Sync: luarocks command not found. Please install LuaRocks.")
		return false
	}

	allOk := true
	for _, pkg := range localPackages {
		packageName := p.getRepo(pkg.SourceID)
		if packageName == "" {
			continue
		}
		packageSpec := packageName
		if pkg.Version != "" && pkg.Version != "latest" {
			packageSpec = fmt.Sprintf("%s %s", packageName, pkg.Version)
		}
		args := []string{"install", packageSpec, "--tree", p.APP_PACKAGES_DIR}
		code, err := luarocksShellOut(luarocksCmd, args, "", nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("LuaRocks Sync: Error installing %s: %v", packageName, err))
			allOk = false
		}
	}

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("LuaRocks Sync: Warning creating wrappers: %v", err))
	}

	return allOk
}
