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

type NuGetProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

var nugetCmd = "dotnet"

// Injectable shell and OS helpers for tests
var nugetShellOut = shell_out.ShellOut
var nugetShellOutCapture = shell_out.ShellOutCapture
var nugetHasCommand = shell_out.HasCommand
var nugetLstat = os.Lstat
var nugetRemove = os.Remove
var nugetChmod = os.Chmod
var nugetStat = os.Stat
var nugetMkdirAll = os.MkdirAll
var nugetWriteFile = os.WriteFile

// Injectable local packages helpers for tests
var lppNugetAdd = local_packages_parser.AddLocalPackage
var lppNugetRemove = local_packages_parser.RemoveLocalPackage
var lppNugetGetDataForProvider = local_packages_parser.GetDataForProvider

func NewProviderNuGet() *NuGetProvider {
	p := &NuGetProvider{}
	p.PROVIDER_NAME = "nuget"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *NuGetProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:nuget/pkg) and new (nuget:pkg) formats
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

func (p *NuGetProvider) Install(sourceID, version string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("NuGet Install: Invalid source ID format")
		return false
	}

	if !nugetHasCommand("dotnet", []string{"--version"}, nil) {
		Logger.Error("NuGet Install: dotnet command not found. Please install .NET SDK.")
		return false
	}

	// Ensure packages directory exists
	if err := nugetMkdirAll(p.APP_PACKAGES_DIR, 0755); err != nil {
		Logger.Error(fmt.Sprintf("NuGet Install: Error creating packages directory: %v", err))
		return false
	}

	// Create a temporary project file for tool installation
	projectPath := filepath.Join(p.APP_PACKAGES_DIR, "nvpm-tools.csproj")
	projectContent := `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <Nullable>enable</Nullable>
  </PropertyGroup>
</Project>`

	// Ensure project file exists
	if _, err := nugetStat(projectPath); os.IsNotExist(err) {
		if err := nugetWriteFile(projectPath, []byte(projectContent), 0644); err != nil {
			Logger.Error(fmt.Sprintf("NuGet Install: Error creating project file: %v", err))
			return false
		}
	}

	// Build dotnet tool install command
	args := []string{"tool", "install", packageName, "--tool-path", p.APP_PACKAGES_DIR}
	if version != "" && version != "latest" {
		args = append(args, "--version", version)
	}

	Logger.Info(fmt.Sprintf("NuGet Install: Installing %s@%s", packageName, version))
	code, err := nugetShellOut(nugetCmd, args, p.APP_PACKAGES_DIR, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("NuGet Install: Error installing tool: %v", err))
		return false
	}

	// Get installed version
	installedVersion := version
	if installedVersion == "" || installedVersion == "latest" {
		// Try to get the installed version using dotnet tool list
		code, output, err := nugetShellOutCapture(nugetCmd, []string{"tool", "list", "--tool-path", p.APP_PACKAGES_DIR}, "", nil)
		if err == nil && code == 0 {
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				if strings.Contains(line, packageName) {
					// Parse line like "package-name  1.2.3"
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
	if err := lppNugetAdd(sourceID, installedVersion); err != nil {
		Logger.Error(fmt.Sprintf("NuGet Install: Error adding package to local packages: %v", err))
		return false
	}

	// Create wrappers for binaries
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("NuGet Install: Warning creating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("NuGet Install: Successfully installed %s@%s", packageName, installedVersion))
	return true
}

func (p *NuGetProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("NuGet Remove: Invalid source ID format")
		return false
	}

	if !nugetHasCommand("dotnet", []string{"--version"}, nil) {
		Logger.Error("NuGet Remove: dotnet command not found. Please install .NET SDK.")
		return false
	}

	Logger.Info(fmt.Sprintf("NuGet Remove: Removing %s", packageName))

	// Remove wrappers
	if err := p.removeWrappersForPackage(packageName); err != nil {
		Logger.Info(fmt.Sprintf("NuGet Remove: Warning removing wrappers: %v", err))
	}

	// Uninstall tool
	code, err := nugetShellOut(nugetCmd, []string{"tool", "uninstall", packageName, "--tool-path", p.APP_PACKAGES_DIR}, p.APP_PACKAGES_DIR, nil)
	if err != nil || code != 0 {
		Logger.Info(fmt.Sprintf("NuGet Remove: Warning uninstalling tool (may not be installed): %v", err))
	}

	// Remove from local packages
	if err := lppNugetRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("NuGet Remove: Error removing package from local packages: %v", err))
		return false
	}

	Logger.Info(fmt.Sprintf("NuGet Remove: Successfully removed %s", packageName))
	return true
}

func (p *NuGetProvider) Update(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	if packageName == "" {
		Logger.Error("NuGet Update: Invalid source ID format")
		return false
	}

	if !nugetHasCommand("dotnet", []string{"--version"}, nil) {
		Logger.Error("NuGet Update: dotnet command not found. Please install .NET SDK.")
		return false
	}

	Logger.Info(fmt.Sprintf("NuGet Update: Updating %s", packageName))

	// Update tool
	code, err := nugetShellOut(nugetCmd, []string{"tool", "update", packageName, "--tool-path", p.APP_PACKAGES_DIR}, p.APP_PACKAGES_DIR, nil)
	if err != nil || code != 0 {
		Logger.Error(fmt.Sprintf("NuGet Update: Error updating tool: %v", err))
		return false
	}

	// Get updated version
	var updatedVersion string
	code, output, err := nugetShellOutCapture(nugetCmd, []string{"tool", "list", "--tool-path", p.APP_PACKAGES_DIR}, "", nil)
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
	if err := lppNugetRemove(sourceID); err == nil {
		if err := lppNugetAdd(sourceID, updatedVersion); err != nil {
			Logger.Error(fmt.Sprintf("NuGet Update: Error updating package in local packages: %v", err))
			return false
		}
	}

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("NuGet Update: Warning recreating wrappers: %v", err))
	}

	Logger.Info(fmt.Sprintf("NuGet Update: Successfully updated %s@%s", packageName, updatedVersion))
	return true
}

func (p *NuGetProvider) getLatestVersion(packageName string) (string, error) {
	if !nugetHasCommand("dotnet", []string{"--version"}, nil) {
		return "", fmt.Errorf("dotnet command not found")
	}

	// Use dotnet tool search or NuGet API
	// For now, we'll use dotnet tool search
	code, output, err := nugetShellOutCapture(nugetCmd, []string{"tool", "search", packageName}, "", nil)
	if err != nil || code != 0 {
		return "", fmt.Errorf("failed to search for tool: %v", err)
	}

	// Parse output to find version
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, packageName) {
			// Output format: "package-name  1.2.3  description"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}

	return "", fmt.Errorf("version not found")
}

// findNuGetBinDir finds the NuGet tools bin directory
func (p *NuGetProvider) findNuGetBinDir() string {
	// Dotnet tools install to: APP_PACKAGES_DIR
	return p.APP_PACKAGES_DIR
}

// createWrappers creates wrapper scripts for NuGet tool executables
func (p *NuGetProvider) createWrappers() error {
	desired := lppNugetGetDataForProvider("nuget").Packages
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
			if _, err := nugetLstat(wrapperPath); err == nil {
				_ = nugetRemove(wrapperPath)
			}
			if err := p.createNuGetWrapperForCommand(binCmd, wrapperPath); err != nil {
				Logger.Error(fmt.Sprintf("Error creating wrapper for %s: %v", binName, err))
				continue
			}
			if err := nugetChmod(wrapperPath, 0755); err != nil {
				Logger.Error(fmt.Sprintf("Error setting executable permissions for %s: %v", binName, err))
			}
		}
	}
	return nil
}

// createNuGetWrapperForCommand creates a wrapper that prepares the environment and executes the given command
func (p *NuGetProvider) createNuGetWrapperForCommand(commandToExec string, wrapperPath string) error {
	nugetBinDir := p.findNuGetBinDir()
	if commandToExec == "" {
		return fmt.Errorf("empty command for wrapper %s", wrapperPath)
	}

	// The command might be a path like "nuget:tool-name" or just "tool-name"
	var execCmd string
	if strings.HasPrefix(commandToExec, "nuget:") {
		binName := strings.TrimPrefix(commandToExec, "nuget:")
		execCmd = filepath.Join(nugetBinDir, binName)
	} else {
		execCmd = filepath.Join(nugetBinDir, commandToExec)
	}

	wrapperContent := fmt.Sprintf(`#!/bin/sh
# Sets up .NET/NuGet environment for nvpm-installed packages and runs the target command

# Add the nvpm NuGet tools directory to PATH
export PATH="%s:$PATH"

# Execute the command from registry
exec %s "$@"
`, nugetBinDir, execCmd)

	if err := nugetWriteFile(wrapperPath, []byte(wrapperContent), 0755); err != nil {
		return err
	}
	return nil
}

// removeWrappersForPackage removes wrapper scripts for a specific package
func (p *NuGetProvider) removeWrappersForPackage(packageName string) error {
	desired := lppNugetGetDataForProvider("nuget").Packages
	nvpmBinDir := files.GetAppBinPath()
	parser := registry_parser.NewDefaultRegistryParser()

	for _, pkg := range desired {
		if p.getRepo(pkg.SourceID) != packageName {
			continue
		}
		registryItem := parser.GetBySourceId(pkg.SourceID)
		for binName := range registryItem.Bin {
			wrapperPath := filepath.Join(nvpmBinDir, binName)
			if _, err := nugetLstat(wrapperPath); err == nil {
				if err := nugetRemove(wrapperPath); err != nil {
					Logger.Info(fmt.Sprintf("NuGet: Warning removing wrapper %s: %v", wrapperPath, err))
				}
			}
		}
	}
	return nil
}

func (p *NuGetProvider) Sync() bool {
	Logger.Info("NuGet Sync: Syncing NuGet packages")
	localPackages := lppNugetGetDataForProvider(p.PROVIDER_NAME).Packages

	if len(localPackages) == 0 {
		return true
	}

	// Check for dotnet command before proceeding
	if !nugetHasCommand("dotnet", []string{"--version"}, nil) {
		Logger.Error("NuGet Sync: dotnet command not found. Please install .NET SDK.")
		return false
	}

	// Reinstall all tools
	for _, pkg := range localPackages {
		packageName := p.getRepo(pkg.SourceID)
		if packageName == "" {
			continue
		}
		args := []string{"tool", "install", packageName, "--tool-path", p.APP_PACKAGES_DIR}
		if pkg.Version != "" && pkg.Version != "latest" {
			args = append(args, "--version", pkg.Version)
		}
		code, err := nugetShellOut(nugetCmd, args, p.APP_PACKAGES_DIR, nil)
		if err != nil || code != 0 {
			Logger.Error(fmt.Sprintf("NuGet Sync: Error installing %s: %v", packageName, err))
			return false
		}
	}

	// Recreate wrappers
	if err := p.createWrappers(); err != nil {
		Logger.Info(fmt.Sprintf("NuGet Sync: Warning creating wrappers: %v", err))
	}

	return true
}
