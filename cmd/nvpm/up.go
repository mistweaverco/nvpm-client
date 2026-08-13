package nvpm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/semver"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
	"github.com/mistweaverco/nvpm-client/internal/lib/version"
	"github.com/spf13/cobra"
)

// UpdateService handles update operations with dependency injection
type UpdateService struct {
	localPackages LocalPackagesProvider
	registry      RegistryProvider
	updateChecker UpdateChecker
	output        OutputWriter
}

// OutputWriter defines the interface for writing output (for testing)
type OutputWriter interface {
	Println(args ...interface{})
	Printf(format string, args ...interface{})
}

// DefaultOutputWriter implements OutputWriter using fmt
type DefaultOutputWriter struct{}

func (d *DefaultOutputWriter) Println(args ...interface{}) {
	fmt.Println(args...)
}

func (d *DefaultOutputWriter) Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// Mock implementations for testing
type MockPackageManager struct {
	UpdateFunc              func(sourceID string) bool
	IsSupportedProviderFunc func(provider string) bool
	AvailableProvidersFunc  func() []string
}

func (m *MockPackageManager) Update(sourceID string) bool {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(sourceID)
	}
	return false
}

func (m *MockPackageManager) IsSupportedProvider(provider string) bool {
	if m.IsSupportedProviderFunc != nil {
		return m.IsSupportedProviderFunc(provider)
	}
	return false
}

func (m *MockPackageManager) AvailableProviders() []string {
	if m.AvailableProvidersFunc != nil {
		return m.AvailableProvidersFunc()
	}
	return []string{}
}

// MockOutputWriter is a mock implementation for testing
type MockOutputWriter struct {
	PrintlnFunc func(args ...interface{})
	PrintfFunc  func(format string, args ...interface{})
	Output      []string
}

func (m *MockOutputWriter) Println(args ...interface{}) {
	if m.PrintlnFunc != nil {
		m.PrintlnFunc(args...)
	}
	// Capture output for testing
	if len(args) > 0 {
		m.Output = append(m.Output, fmt.Sprint(args...))
	}
}

func (m *MockOutputWriter) Printf(format string, args ...interface{}) {
	if m.PrintfFunc != nil {
		m.PrintfFunc(format, args...)
	}
	// Capture output for testing
	m.Output = append(m.Output, fmt.Sprintf(format, args...))
}

// NewUpdateService creates a new UpdateService with default dependencies
func NewUpdateService() *UpdateService {
	return &UpdateService{
		localPackages: &defaultLocalPackagesProvider{},
		registry:      &defaultRegistryProvider{},
		updateChecker: &defaultUpdateChecker{},
		output:        &DefaultOutputWriter{},
	}
}

// NewUpdateServiceWithDependencies creates a new UpdateService with custom dependencies
func NewUpdateServiceWithDependencies(
	localPackages LocalPackagesProvider,
	registry RegistryProvider,
	updateChecker UpdateChecker,
	output OutputWriter,
) *UpdateService {
	return &UpdateService{
		localPackages: localPackages,
		registry:      registry,
		updateChecker: updateChecker,
		output:        output,
	}
}

// updatePackage updates a single package using the provider factory system
func (us *UpdateService) updatePackage(sourceID string) bool {
	// Use the provider factory system which can be mocked in tests
	return providers.Update(sourceID)
}

var upCmd = &cobra.Command{
	Use:     "up",
	Aliases: []string{"update"},
	Short:   "Update packages to their latest versions",
	Long: `Update packages to their latest versions.

Examples:
  nvpm up npm:eslint
  nvpm up golang:golang.org/x/tools/gopls npm:prettier
  nvpm up pypi:black cargo:ripgrep
  nvpm up github:user/repo gitlab:group/subgroup/project
  nvpm up --all (update all installed packages)
  nvpm up --self (update nvpm itself to the latest version)

Note: pinned versions like pkg@v1.2.3 are not supported by up.
Use nvpm add / nvpm set to install or switch to a specific version.`,
	Args: cobra.MinimumNArgs(0), // Allow no args if --all or --self is used
	// Enable shell completion for installed package IDs only.
	ValidArgsFunction: installedPackageIDCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateAlwaysTrustFlags(); err != nil {
			service := newUpdateService()
			service.output.Printf("%s %v\n", IconClose(), err)
			osExit(1)
			return
		}
		forceFlag, _ := cmd.Flags().GetBool("force")
		providers.SetMinReleaseAgePolicy(providers.MinReleaseAgePolicy{
			MinAge: cfg.Flags.MinReleaseAge,
			Force:  forceFlag,
		})
		selfFlag, _ := cmd.Flags().GetBool("self")
		if selfFlag {
			service := newUpdateService()
			if err := runSelfUpdate(service.output); err != nil {
				service.output.Printf("%s Failed to update nvpm: %v\n", IconClose(), err)
				osExit(1)
				return
			}
			return
		}

		allFlag, _ := cmd.Flags().GetBool("all")

		if allFlag {
			// Update all installed packages
			service := newUpdateService()
			service.output.Println("Updating all installed packages to latest versions...")

			success := service.UpdateAllPackages(getShowFilters(cmd))

			if !success {
				service.output.Println("Failed to update some packages")
			}
			return
		}

		// Check if package IDs are provided
		if len(args) == 0 {
			service := newUpdateService()
			service.output.Println("Error: Please provide package IDs or use --all flag")
			return
		}

		// Process all packages
		packages := args
		internalIDs := make([]string, 0, len(packages))
		displayIDs := make([]string, 0, len(packages))
		skippedPinned := make([]string, 0)

		for _, userPkgID := range packages {
			// Parse package ID and version from the user-facing ID
			baseID, pinnedVersion := parsePackageIDAndVersion(userPkgID)
			if pinnedVersion != "" {
				skippedPinned = append(skippedPinned, userPkgID)
				continue
			}

			var internalID string
			var displayID string

			// Check if this is a package name without provider
			if !strings.Contains(baseID, ":") && !strings.HasPrefix(baseID, "pkg:") {
				// Package name without provider - search installed packages and prompt user
				matches := findInstalledPackagesByName(baseID)
				if len(matches) == 0 {
					service := newUpdateService()
					service.output.Printf("%s No installed packages found matching '%s'\n", IconClose(), baseID)
					return
				}

				// Always show confirmation for partial names (user didn't provide full provider:package-id)
				selectedSourceIDs, err := promptForProviderSelection(baseID, matches, "update")
				if err != nil {
					service := newUpdateService()
					service.output.Printf("%s Error selecting provider for '%s': %v\n", IconClose(), baseID, err)
					return
				}

				// Process all selected packages
				for _, selectedSourceID := range selectedSourceIDs {
					internalIDs = append(internalIDs, selectedSourceID)
					displayIDs = append(displayIDs, selectedSourceID)
				}
				continue // Skip the single package processing below
			} else {
				// Package with provider - parse normally
				provider, pkgName, err := parseUserPackageID(baseID)
				if err != nil {
					service := newUpdateService()
					service.output.Printf("Error: %v\n", err)
					return
				}
				if !providers.IsSupportedProvider(provider) {
					service := newUpdateService()
					service.output.Printf("Error: Unsupported provider '%s' for package '%s'. Supported providers: %s\n", provider, userPkgID, strings.Join(providers.AvailableProviders, ", "))
					return
				}

				internalID = toInternalPackageID(provider, pkgName)
				// Construct displayID from provider and package name (full provider:package-id format)
				displayID = fmt.Sprintf("%s:%s", provider, pkgName)
			}

			internalIDs = append(internalIDs, internalID)
			displayIDs = append(displayIDs, displayID)
		}

		service := newUpdateService()
		for _, pinned := range skippedPinned {
			_, version := parsePackageIDAndVersion(pinned)
			service.output.Printf("%s Skipping %s: `nvpm up` cannot target a specific version (%s).\n", IconAlert(), pinned, version)
			service.output.Printf("  To set a specific version, use `nvpm add %s` or `nvpm set %s`.\n", pinned, pinned)
		}

		filters := getShowFilters(cmd)
		if len(filters) > 0 {
			filteredInternal := make([]string, 0, len(internalIDs))
			filteredDisplay := make([]string, 0, len(displayIDs))
			for i, id := range internalIDs {
				if packageMatchesShowFilters(id, filters) {
					filteredInternal = append(filteredInternal, id)
					filteredDisplay = append(filteredDisplay, displayIDs[i])
				}
			}
			internalIDs = filteredInternal
			displayIDs = filteredDisplay
		}

		if len(internalIDs) == 0 {
			if len(skippedPinned) > 0 {
				service.output.Println("No packages updated.")
				return
			}
			service.output.Println("No packages match the current --filter criteria")
			return
		}

		// Update individual packages
		service.output.Printf("Updating %d package(s) to latest versions...\n", len(internalIDs))

		allSuccess := true
		successCount := 0
		failedCount := 0
		waitingCount := 0
		skippedUpToDate := 0
		installedByID := map[string]local_packages_parser.LocalPackageItem{}
		for _, pkg := range service.localPackages.GetData(true).Packages {
			installedByID[pkg.SourceID] = pkg
		}

		for idx := range internalIDs {
			internalID := internalIDs[idx]
			displayID := displayIDs[idx]

			if pkg, ok := installedByID[internalID]; ok {
				if !service.checkUpdateAvailability(internalID, pkg.Version, pkg.Commit) {
					service.output.Printf("%s %s is already up to date\n", IconCheck(), displayID)
					skippedUpToDate++
					clearPendingUpdateResolution(internalID)
					clearPendingAlwaysTrust(internalID)
					continue
				}
			}

			if err := applyPendingUpdateResolution(internalID); err != nil {
				service.output.Printf("%s Invalid --update-resolution: %v\n", IconClose(), err)
				failedCount++
				allSuccess = false
				continue
			}
			applyPendingAlwaysTrust(internalID)

			if err := providers.PreflightRegistryInstallHooksForSource(internalID); err != nil {
				service.output.Printf("%s %v\n", IconClose(), err)
				failedCount++
				allSuccess = false
				clearPendingUpdateResolution(internalID)
				clearPendingAlwaysTrust(internalID)
				continue
			}

			// Update the package with spinner showing package name
			var success bool
			action := func() {
				success = service.updatePackage(internalID)
			}

			title := fmt.Sprintf("Updating %s...", displayID)
			if err := spinnerutil.Run(title, action); err != nil {
				service.output.Printf("%s Failed to update %s: %v\n", IconClose(), displayID, err)
				failedCount++
				allSuccess = false
				clearPendingUpdateResolution(internalID)
				clearPendingAlwaysTrust(internalID)
				continue
			}

			if success {
				service.output.Printf("%s Successfully updated %s\n", IconCheck(), displayID)
				successCount++
				persistUpdateResolutionAfterInstall(internalID)
				persistAlwaysTrustAfterInstall(internalID)
				continue
			}

			if skip := strings.TrimSpace(providers.TakeLastSkip()); skip != "" {
				service.output.Printf("%s Skipped %s\n", IconLightbulb(), displayID)
				service.output.Printf("  %s\n", skip)
				waitingCount++
				clearPendingUpdateResolution(internalID)
				clearPendingAlwaysTrust(internalID)
				continue
			}

			service.output.Printf("%s Failed to update %s\n", IconClose(), displayID)
			if detail := strings.TrimSpace(providers.TakeLastError()); detail != "" {
				service.output.Printf("  %s\n", detail)
			}
			failedCount++
			allSuccess = false
			clearPendingUpdateResolution(internalID)
			clearPendingAlwaysTrust(internalID)
		}

		// Print summary
		service.output.Printf("\nUpdate Summary:\n")
		service.output.Printf("  Successfully updated: %d\n", successCount)
		if skippedUpToDate > 0 {
			service.output.Printf("  Skipped (up to date): %d\n", skippedUpToDate)
		}
		if waitingCount > 0 {
			service.output.Printf("  Skipped (waiting for min-release-age): %d\n", waitingCount)
		}
		service.output.Printf("  Failed to update: %d\n", failedCount)

		if allSuccess {
			if successCount > 0 {
				service.output.Printf("All packages updated successfully!\n")
			} else if waitingCount > 0 {
				service.output.Printf("No packages updated - waiting for min-release-age.\n")
			} else if skippedUpToDate > 0 {
				service.output.Printf("All packages are up to date.\n")
			}
		} else {
			service.output.Printf("Some packages failed to update.\n")
		}
	},
}

func init() {
	upCmd.Flags().BoolP("all", "A", false, "Update all installed packages to their latest versions")
	upCmd.Flags().Bool("self", false, "Update nvpm itself to the latest version")
	upCmd.Flags().Bool("force", false, "bypass min-release-age safety checks")
	upCmd.Flags().BoolVar(&installAlwaysTrust, "always-trust", false, "persistently skip min-release-age for this package (stored in lock extras.always_trust)")
	upCmd.Flags().BoolVar(&installNoAlwaysTrust, "no-always-trust", false, "clear extras.always_trust for this package")
	upCmd.Flags().StringVar(&installUpdateResolution, "update-resolution", "", "git-only: override update-resolution when updating (e.g. always, release-age-gap:30d)")
	registerShowFilterFlag(upCmd)
}

// newUpdateService is a factory to allow test injection
var newUpdateService = NewUpdateService

// UpdateAllPackages updates all installed packages to their latest versions
// Only updates packages that have updates available according to the registry data
// (or cached remote_latest for non-registry git packages).
func (us *UpdateService) UpdateAllPackages(showFilters ...[]string) bool {
	var filters []string
	if len(showFilters) > 0 {
		filters = showFilters[0]
	}
	// Refresh registry and discover non-registry git remotes when the snapshot was rebuilt.
	refreshed, _ := downloadAndUnzipRegistryFn()
	us.registry.GetData(refreshed)

	// Get all installed packages
	localPackages := us.localPackages.GetData(true).Packages
	if refreshed {
		_ = providers.DiscoverNonRegistryGitPackages(localPackages, func(sourceID string) bool {
			stable, prerelease := us.registry.GetLatestVersions(sourceID)
			return strings.TrimSpace(stable) != "" || strings.TrimSpace(prerelease) != ""
		}, !ShouldUseJSONOutput())
	}

	if len(localPackages) == 0 {
		us.output.Println("No packages are currently installed")
		return true
	}

	us.output.Printf("Found %d installed packages\n", len(localPackages))

	// Check which packages have updates available
	packagesToUpdate := make([]local_packages_parser.LocalPackageItem, 0)
	skippedCount := 0

	for _, pkg := range localPackages {
		if len(filters) > 0 && !packageMatchesShowFilters(pkg.SourceID, filters) {
			skippedCount++
			continue
		}
		hasUpdate := us.checkUpdateAvailability(pkg.SourceID, pkg.Version, pkg.Commit)
		if hasUpdate {
			packagesToUpdate = append(packagesToUpdate, pkg)
		} else {
			skippedCount++
		}
	}

	if len(packagesToUpdate) == 0 {
		us.output.Printf("All %d packages are up to date\n", len(localPackages))
		return true
	}

	us.output.Printf("Updating %d package(s) with available updates (skipping %d up-to-date package(s))\n", len(packagesToUpdate), skippedCount)

	allSuccess := true
	successCount := 0
	failedCount := 0
	waitingCount := 0

	for _, pkg := range packagesToUpdate {
		if err := applyPendingUpdateResolution(pkg.SourceID); err != nil {
			us.output.Printf("%s Invalid --update-resolution: %v\n", IconClose(), err)
			failedCount++
			allSuccess = false
			continue
		}
		applyPendingAlwaysTrust(pkg.SourceID)

		if err := providers.PreflightRegistryInstallHooksForSource(pkg.SourceID); err != nil {
			us.output.Printf("%s %v\n", IconClose(), err)
			failedCount++
			allSuccess = false
			clearPendingUpdateResolution(pkg.SourceID)
			clearPendingAlwaysTrust(pkg.SourceID)
			continue
		}

		// Update the package with spinner showing package name
		var success bool
		action := func() {
			success = us.updatePackage(pkg.SourceID)
		}

		title := fmt.Sprintf("Updating %s...", pkg.SourceID)
		if err := spinnerutil.Run(title, action); err != nil {
			us.output.Printf("%s Failed to update %s: %v\n", IconClose(), pkg.SourceID, err)
			failedCount++
			allSuccess = false
			clearPendingUpdateResolution(pkg.SourceID)
			clearPendingAlwaysTrust(pkg.SourceID)
			continue
		}

		if success {
			successCount++
			us.output.Printf("%s Successfully updated %s\n", IconCheck(), pkg.SourceID)
			persistUpdateResolutionAfterInstall(pkg.SourceID)
			persistAlwaysTrustAfterInstall(pkg.SourceID)
			continue
		}

		if skip := strings.TrimSpace(providers.TakeLastSkip()); skip != "" {
			us.output.Printf("%s Skipped %s\n", IconLightbulb(), pkg.SourceID)
			us.output.Printf("  %s\n", skip)
			waitingCount++
			clearPendingUpdateResolution(pkg.SourceID)
			clearPendingAlwaysTrust(pkg.SourceID)
			continue
		}

		failedCount++
		us.output.Printf("%s Failed to update %s\n", IconClose(), pkg.SourceID)
		if detail := strings.TrimSpace(providers.TakeLastError()); detail != "" {
			us.output.Printf("  %s\n", detail)
		}
		allSuccess = false
		clearPendingUpdateResolution(pkg.SourceID)
		clearPendingAlwaysTrust(pkg.SourceID)
	}

	us.output.Printf("\nUpdate Summary:\n")
	us.output.Printf("  Successfully updated: %d\n", successCount)
	if waitingCount > 0 {
		us.output.Printf("  Skipped (waiting for min-release-age): %d\n", waitingCount)
	}
	us.output.Printf("  Failed to update: %d\n", failedCount)
	us.output.Printf("  Skipped (up to date): %d\n", skippedCount)

	return allSuccess
}

// checkUpdateAvailability checks if an update is available for a package
func (us *UpdateService) checkUpdateAvailability(sourceID, currentVersion, installedCommit string) bool {
	stable, prerelease, remoteCommit := resolveUpdateCandidates(us.registry, sourceID)
	if stable == "" && prerelease == "" {
		// No registry or remote-latest info available - skip update check
		return false
	}
	latestVersion := chooseBestRemoteVersion(currentVersion, stable, prerelease)
	if providers.GitCommitStillNeedsUpdate(sourceID, latestVersion, installedCommit, remoteCommit) {
		return true
	}
	if strings.TrimSpace(installedCommit) != "" && strings.TrimSpace(remoteCommit) != "" {
		return false
	}
	// If local version is unknown or set to "latest", always show update to the concrete remote version
	if currentVersion == "" || currentVersion == "latest" {
		return true
	}
	updateAvailable, _ := us.updateChecker.CheckIfUpdateIsAvailable(currentVersion, latestVersion)
	return updateAvailable
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// getCurrentVersion returns the current version of nvpm
func getCurrentVersion() string {
	return version.VERSION
}

// getLatestVersion fetches the latest release version from GitHub
func getLatestVersion() (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Get the latest release
	resp, err := client.Get("https://api.github.com/repos/mistweaverco/nvpm-client/releases/latest")
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("failed to parse release data: %w", err)
	}

	return release.TagName, nil
}

// getCurrentBinaryPath returns the path to the current nvpm binary
func getCurrentBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the actual file path
	resolvedPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		// If symlink resolution fails, use the original path
		resolvedPath = execPath
	}

	return resolvedPath, nil
}

// detectPlatform returns the platform string for the current system
func detectPlatform() string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	// Map Go architecture names to release asset names
	switch arch {
	case "amd64":
		arch = "amd64"
	case "386":
		arch = "386"
	case "arm64":
		arch = "arm64"
	case "arm":
		arch = "armv7"
	default:
		arch = "amd64" // fallback
	}

	return fmt.Sprintf("%s-%s", os, arch)
}

// downloadBinary downloads the specified version of nvpm for the current platform
func downloadBinary(version, platform string) (string, error) {
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Construct download URL
	fileName := fmt.Sprintf("nvpm-%s", platform)
	if platform == "windows-amd64" || platform == "windows-386" {
		fileName += ".exe"
	}

	downloadURL := fmt.Sprintf("https://github.com/mistweaverco/nvpm-client/releases/download/%s/%s", version, fileName)

	// Create temporary file
	tempFile, err := os.CreateTemp("", "nvpm-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tempFile.Close()

	// Download the binary
	resp, err := client.Get(downloadURL)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to download binary: HTTP %d", resp.StatusCode)
	}

	// Copy the response to the temporary file
	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to save binary: %w", err)
	}

	// Make the file executable on Unix-like systems
	if platform != "windows-amd64" && platform != "windows-386" {
		if err := os.Chmod(tempFile.Name(), 0755); err != nil {
			os.Remove(tempFile.Name())
			return "", fmt.Errorf("failed to make binary executable: %w", err)
		}
	}

	return tempFile.Name(), nil
}

// backupCurrentBinary creates a backup of the current binary
func backupCurrentBinary(binaryPath string) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", binaryPath, timestamp)

	if err := copyFile(binaryPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	return backupPath, nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Copy file permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	return os.Chmod(dst, sourceInfo.Mode())
}

// replaceBinary replaces the current binary with the new one
func replaceBinary(currentPath, newBinaryPath string) error {
	// Remove the current binary
	if err := os.Remove(currentPath); err != nil {
		return fmt.Errorf("failed to remove current binary: %w", err)
	}

	// Copy the new binary to the current location
	if err := copyFile(newBinaryPath, currentPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	return nil
}

// runSelfUpdate executes the self-update process
func runSelfUpdate(output OutputWriter) error {
	// Get current version
	currentVersion := getCurrentVersion()
	if currentVersion == "" {
		return fmt.Errorf("current version is not set")
	}

	output.Printf("Current version: %s\n", currentVersion)

	// Get latest version
	output.Printf("Checking for updates...\n")
	latestVersion, err := getLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to get latest version: %w", err)
	}

	// Compare versions
	if !semver.IsGreater(currentVersion, latestVersion) {
		output.Printf("nvpm is already up to date (version %s)\n", currentVersion)
		return nil
	}

	output.Printf("New version available: %s (current: %s)\n", latestVersion, currentVersion)

	// Get current binary path
	currentPath, err := getCurrentBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get current binary path: %w", err)
	}

	// Detect platform
	platform := detectPlatform()
	output.Printf("Detected platform: %s\n", platform)

	// Download new binary with spinner
	var newBinaryPath string
	var downloadErr error
	action := func() {
		newBinaryPath, downloadErr = downloadBinary(latestVersion, platform)
	}

	title := fmt.Sprintf("Downloading nvpm %s for %s...", latestVersion, platform)
	if err := spinnerutil.Run(title, action); err != nil {
		return fmt.Errorf("failed to download new version: %w", err)
	}

	if downloadErr != nil {
		return fmt.Errorf("failed to download new version: %w", downloadErr)
	}
	defer os.Remove(newBinaryPath) // Clean up temp file

	// Create backup
	output.Printf("Creating backup of current binary...\n")
	backupPath, err := backupCurrentBinary(currentPath)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	output.Printf("Backup created: %s\n", backupPath)

	// Replace binary
	output.Printf("Installing new version...\n")
	if err := replaceBinary(currentPath, newBinaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	output.Printf("%s Successfully updated nvpm from %s to %s\n", IconCheck(), currentVersion, latestVersion)
	output.Printf("Backup saved as: %s\n", backupPath)

	return nil
}
