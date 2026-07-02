package nvpm

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/spf13/cobra"
)

// parseUserPackageID parses a user-facing package ID and returns
// the provider and raw package identifier (without version).
//
// It supports both the legacy format:
//
//	pkg:<provider>/<package-name>
//
// and the new simplified format:
//
//	<provider>:<package-name>
func parseUserPackageID(arg string) (string, string, error) {
	// Legacy format: pkg:provider/name
	if strings.HasPrefix(arg, "pkg:") {
		parts := strings.Split(strings.TrimPrefix(arg, "pkg:"), "/")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid package ID format '%s': expected 'pkg:provider/package-name[@version]'", arg)
		}
		provider := parts[0]
		if provider == "" {
			return "", "", fmt.Errorf("invalid package ID format '%s': provider cannot be empty", arg)
		}
		packageName := parts[1]
		if packageName == "" {
			return "", "", fmt.Errorf("invalid package ID format '%s': package name cannot be empty", arg)
		}
		return provider, packageName, nil
	}

	// New format: provider:package-name
	if !strings.Contains(arg, ":") {
		return "", "", fmt.Errorf("invalid package ID format '%s': expected '<provider>:<package-id>[@version]'", arg)
	}

	parts := strings.SplitN(arg, ":", 2)
	provider := parts[0]
	packageName := parts[1]

	if provider == "" {
		return "", "", fmt.Errorf("invalid package ID format '%s': provider cannot be empty", arg)
	}
	if packageName == "" {
		return "", "", fmt.Errorf("invalid package ID format '%s': package id cannot be empty", arg)
	}

	return provider, packageName, nil
}

// toInternalPackageID normalizes a user-facing package ID to the
// internal representation "<provider>:<package-id>".
// This is the format used in nvpm-lock.json and throughout the codebase.
func toInternalPackageID(provider, pkgID string) string {
	return provider + ":" + pkgID
}

// validatePackageArgs validates the package arguments (for cobra)
// and ensures that all providers are supported.
// It allows package names without providers (they will be handled in Run function).
func validatePackageArgs(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("requires at least 1 argument")
	}

	for _, arg := range args {
		// Check if it's a package name without provider (no colon and no pkg: prefix)
		if !strings.Contains(arg, ":") && !strings.HasPrefix(arg, "pkg:") {
			// Allow it - will be handled in Run function
			continue
		}

		provider, _, err := parseUserPackageID(arg)
		if err != nil {
			return err
		}

		if !providers.IsSupportedProvider(provider) {
			return fmt.Errorf("unsupported provider '%s' for package '%s'. Supported providers: %s",
				provider, arg, strings.Join(providers.AvailableProviders, ", "))
		}
	}

	return nil
}

var installCmd = &cobra.Command{
	Use:     "install <pkgId> [pkgId...]",
	Aliases: []string{"add"},
	Short:   "Install one or more packages",
	Long: `Install one or more packages from supported providers.

Supported package ID formats:
  npm:@prisma/language-server
  npm:@prisma/language-server@latest
  golang:golang.org/x/tools/gopls@v0.14.0
  pypi:black@22.3.0
  cargo:ripgrep@13.0.0
  github:user/repo
  github:user/repo@v1.0.0
  gitlab:group/subgroup/project
  gitlab:group/subgroup/project@v1.0.0
  codeberg:user/repo
  codeberg:user/repo@v1.0.0

Examples:
  nvpm install npm:@prisma/language-server
  nvpm install golang:golang.org/x/tools/gopls@latest
  nvpm install npm:eslint pypi:black@22.3.0
  nvpm install cargo:ripgrep@13.0.0 npm:prettier
  nvpm install github:sharkdp/bat
  nvpm install gitlab:group/subgroup/myproject@v1.0.0
  nvpm install codeberg:user/repo`,
	Args: func(cmd *cobra.Command, args []string) error {
		return validatePackageArgs(args)
	},
	// Enable shell completion for package IDs based on the local registry.
	ValidArgsFunction: packageIDCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		providers.SetMinReleaseAgePolicy(providers.MinReleaseAgePolicy{
			MinAge: cfg.Flags.MinReleaseAge,
			Force:  installForce,
		})
		if err := providers.ConfigureExternalTreeSitterQueriesFromCLI(
			cmd.Flags().Changed("external-treesitter-queries"),
			installExternalTreeSitterQueries,
		); err != nil {
			fmt.Printf("%s Invalid --external-treesitter-queries: %v\n", IconClose(), err)
			osExit(1)
			return
		}
		userIntegrations := append([]string(nil), installIntegrations...)
		providers.SetRequestedIntegrations(userIntegrations)
		providers.ResetTreeSitterDependencyInstallSuccessCount()

		cleanupNestedInstallOutput := registerNestedInstallOutputHooks()
		defer cleanupNestedInstallOutput()

		// Install all packages
		successCount := 0
		failureCount := 0
		var failures []string

		for _, userPkgID := range args {
			// Parse package ID and version from the user-facing ID
			baseID, version := parsePackageIDAndVersion(userPkgID)

			var internalID string
			var displayID string

			// Check if this is a package name without provider
			if !strings.Contains(baseID, ":") && !strings.HasPrefix(baseID, "pkg:") {
				// Package name without provider - search registry and prompt user
				// Always show confirmation for partial names (user didn't provide full provider:package-id)
				matches := findPackagesByName(baseID)
				if len(matches) == 0 {
					fmt.Printf("%s No packages found matching '%s'\n", IconClose(), baseID)
					failureCount++
					failures = append(failures, userPkgID)
					continue
				}

				// Filter matches to exact package name or alias matches first (for better UX)
				exactMatches := []PackageMatch{}
				partialMatches := []PackageMatch{}
				baseIDLower := strings.ToLower(baseID)
				parser := newRegistryParser()

				for _, match := range matches {
					matchNameLower := strings.ToLower(match.PackageName)
					// Check if package name matches exactly
					isExactMatch := matchNameLower == baseIDLower

					// Also check if any alias matches exactly
					if !isExactMatch {
						registryItem := parser.GetBySourceId(match.SourceID)
						if registryItem.Source.ID != "" {
							for _, alias := range registryItem.Aliases {
								if strings.ToLower(alias) == baseIDLower {
									isExactMatch = true
									break
								}
							}
						}
					}

					if isExactMatch {
						exactMatches = append(exactMatches, match)
					} else {
						partialMatches = append(partialMatches, match)
					}
				}

				// Use exact matches if available, otherwise use partial matches
				matchesToShow := exactMatches
				if len(exactMatches) == 0 {
					matchesToShow = partialMatches
				}

				// Always show confirmation for partial names
				selectedSourceIDs, err := promptForProviderSelection(baseID, matchesToShow, "install")
				if err != nil {
					fmt.Printf("%s Error selecting provider for '%s': %v\n", IconClose(), baseID, err)
					failureCount++
					failures = append(failures, userPkgID)
					continue
				}

				// Process all selected packages
				for _, selectedSourceID := range selectedSourceIDs {
					internalID := selectedSourceID
					// selectedSourceID is already in provider:package-id format, use it directly
					displayID := selectedSourceID

					// Resolve version before installing to show actual version in spinner
					resolvedVersion, err := resolveVersionFn(internalID, version)
					if err != nil {
						fmt.Printf("%s Failed to resolve version for %s: %v\n", IconClose(), displayID, err)
						failureCount++
						failures = append(failures, displayID)
						continue
					}

					registryItem := newRegistryParser().GetBySourceId(internalID)

					effectiveIntegrations, err := providers.ResolveTreeSitterInstallIntegrations(
						registryItem,
						userIntegrations,
						providers.TreeSitterIntegrateResolveOpts{
							MachineOutput: ShouldUseJSONOutput() || ShouldUsePlainOutput(),
						},
					)
					if err != nil {
						fmt.Printf("%s %v\n", IconClose(), err)
						failureCount++
						failures = append(failures, displayID)
						continue
					}
					providers.SetRequestedIntegrations(effectiveIntegrations)

					title := fmt.Sprintf("Installing %s@%s...", displayID, resolvedVersion)
					success, err := runNvpmInstallWithTreeSitterSpinnerPhases(title, internalID, resolvedVersion, registryItem, func() bool {
						return installPackageFn(internalID, resolvedVersion)
					})
					providers.SetRequestedIntegrations(userIntegrations)
					if err != nil {
						failureCount++
						failures = append(failures, displayID)
						fmt.Printf("%s Failed to install %s@%s: %v\n", IconClose(), displayID, resolvedVersion, err)
						continue
					}

					if success {
						successCount++
						_ = local_packages_parser.MergePackageIntegrations(internalID, effectiveIntegrations)
						fmt.Printf("%s Successfully installed %s@%s\n", IconCheck(), displayID, resolvedVersion)
						for _, line := range providers.ConsumeIntegrationReport(internalID, resolvedVersion) {
							fmt.Printf("  %s@%s: %s\n", internalID, resolvedVersion, line)
						}
					} else {
						failureCount++
						failures = append(failures, displayID)
						fmt.Printf("%s Failed to install %s@%s\n", IconClose(), displayID, resolvedVersion)
					}
				}
				continue // Skip the single package processing below
			} else {
				// Package with provider - parse normally
				// Full provider:package-id provided, no confirmation needed
				provider, pkgName, err := parseUserPackageID(baseID)
				if err != nil {
					// This shouldn't happen because Args validation already ran,
					// but guard just in case.
					fmt.Printf("Error: %v\n", err)
					return
				}

				internalID = toInternalPackageID(provider, pkgName)
				// Construct displayID from provider and package name (will add resolved version later)
				displayID = fmt.Sprintf("%s:%s", provider, pkgName)
			}

			// Resolve version before installing to show actual version in spinner
			resolvedVersion, err := resolveVersionFn(internalID, version)
			if err != nil {
				fmt.Printf("%s Failed to resolve version for %s: %v\n", IconClose(), displayID, err)
				failureCount++
				failures = append(failures, displayID)
				continue
			}

			registryItem := newRegistryParser().GetBySourceId(internalID)

			effectiveIntegrations, err := providers.ResolveTreeSitterInstallIntegrations(
				registryItem,
				userIntegrations,
				providers.TreeSitterIntegrateResolveOpts{
					MachineOutput: ShouldUseJSONOutput() || ShouldUsePlainOutput(),
				},
			)
			if err != nil {
				fmt.Printf("%s %v\n", IconClose(), err)
				failureCount++
				failures = append(failures, displayID)
				continue
			}
			providers.SetRequestedIntegrations(effectiveIntegrations)

			title := fmt.Sprintf("Installing %s@%s...", displayID, resolvedVersion)
			success, err := runNvpmInstallWithTreeSitterSpinnerPhases(title, internalID, resolvedVersion, registryItem, func() bool {
				return installPackageFn(internalID, resolvedVersion)
			})
			providers.SetRequestedIntegrations(userIntegrations)
			if err != nil {
				failureCount++
				failures = append(failures, displayID)
				fmt.Printf("%s Failed to install %s@%s: %v\n", IconClose(), displayID, resolvedVersion, err)
				continue
			}

			if success {
				successCount++
				_ = local_packages_parser.MergePackageIntegrations(internalID, effectiveIntegrations)
				fmt.Printf("%s Successfully installed %s@%s\n", IconCheck(), displayID, resolvedVersion)
				for _, line := range providers.ConsumeIntegrationReport(internalID, resolvedVersion) {
					fmt.Printf("  %s@%s: %s\n", internalID, resolvedVersion, line)
				}
			} else {
				failureCount++
				failures = append(failures, displayID)
				fmt.Printf("%s Failed to install %s@%s\n", IconClose(), displayID, resolvedVersion)
			}
		}

		depSuccess := providers.ConsumeTreeSitterDependencyInstallSuccessCount()
		totalSuccess := successCount + depSuccess

		// Print summary
		if ShouldUseJSONOutput() {
			result := map[string]interface{}{
				"success_count":            totalSuccess,
				"direct_success_count":     successCount,
				"dependency_success_count": depSuccess,
				"failure_count":            failureCount,
				"successful":               totalSuccess,
				"direct_successful":        successCount,
				"dependency_successful":    depSuccess,
				"failed":                   failures,
			}
			PrintJSON(result)
		} else {
			fmt.Printf("\nInstallation Summary:\n")
			fmt.Printf("  Successfully installed: %d", totalSuccess)
			if depSuccess > 0 {
				if successCount > 0 {
					fmt.Printf(" (%d you requested", successCount)
					if depSuccess == 1 {
						fmt.Printf(", 1 dependency")
					} else {
						fmt.Printf(", %d dependencies", depSuccess)
					}
					fmt.Printf(")\n")
				} else {
					if depSuccess == 1 {
						fmt.Printf(" (1 dependency)\n")
					} else {
						fmt.Printf(" (%d dependencies)\n", depSuccess)
					}
				}
			} else {
				fmt.Printf("\n")
			}
			if failureCount > 0 {
				fmt.Printf("  Failed to install: %d\n", failureCount)
				fmt.Printf("  Failed packages: %s\n", strings.Join(failures, ", "))
			}
		}
	},
}

var installIntegrations []string
var installExternalTreeSitterQueries string
var installForce bool

func init() {
	installCmd.Flags().StringSliceVar(&installIntegrations, "integrate", nil, "run integration backends after install (e.g. --integrate neovim)")
	installCmd.Flags().StringVar(&installExternalTreeSitterQueries, "external-treesitter-queries", "ask", "when Neovim integration needs optional query-only git repos from the registry: ask (default), always, never (overridden by NVPM_EXTERNAL_TREESITTER_QUERIES when this flag is left at default)")
	installCmd.Flags().BoolVar(&installForce, "force", false, "bypass min-release-age safety checks")
}

// indirections for testability
var (
	isSupportedProviderFn = providers.IsSupportedProvider
	availableProvidersFn  = func() []string { return providers.AvailableProviders }
	installPackageFn      = providers.Install
	resolveVersionFn      = providers.ResolveVersion
)

// isValidVersionString checks if a string looks like a valid version
func isValidVersionString(version string) bool {
	// Common version patterns: "1.0.0", "latest", "v1.0.0", "1.0.0-beta", etc.
	// A version should contain at least one digit or be "latest"
	if version == "latest" {
		return true
	}

	// Common git branch names users pass explicitly (mainly for VCS providers)
	switch strings.ToLower(version) {
	case "main", "master", "trunk", "head":
		return true
	}

	// Git commit SHA (short or full)
	isHex := true
	if len(version) >= 7 && len(version) <= 40 {
		for _, char := range version {
			isHex = (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')
			if !isHex {
				break
			}
		}
		if isHex {
			return true
		}
	}

	// Check if it contains at least one digit
	for _, char := range version {
		if char >= '0' && char <= '9' {
			return true
		}
	}

	return false
}

// parsePackageIDAndVersion extracts the package ID and version from a full package ID string.
// It handles the format pkg:provider/name[@version] where name can contain @ symbols.
func parsePackageIDAndVersion(pkgId string) (string, string) {
	// Split by @ and check if the last part looks like a version
	parts := strings.Split(pkgId, "@")
	if len(parts) > 1 {
		lastPart := parts[len(parts)-1]
		// Check if the last part looks like a version (contains digits or is "latest")
		if isValidVersionString(lastPart) {
			// Reconstruct the package name without the version
			packageName := strings.Join(parts[:len(parts)-1], "@")
			return packageName, lastPart
		}
	}
	// No valid version found, return the full package ID with empty version.
	// Empty version means "use the registry default if present, otherwise provider default".
	return pkgId, ""
}

// PackageMatch represents a package found in the registry
type PackageMatch struct {
	SourceID    string
	Provider    string
	PackageName string
	Name        string
	Description string
	Version     string
}

// findPackagesByName searches the registry for packages matching the given name
// (substring match, case-insensitive) and returns matches grouped by provider.
// It matches both package names and aliases.
func findPackagesByName(packageName string) []PackageMatch {
	parser := newRegistryParser()
	items := parser.GetData(false)

	matches := []PackageMatch{}
	packageNameLower := strings.ToLower(packageName)

	for _, item := range items {
		// Extract provider and package name from source ID
		sourceID := strings.TrimSpace(item.Source.ID)
		if sourceID == "" {
			continue
		}

		// Get package name without provider
		displayID := displayPackageNameFromRegistryID(sourceID)
		if displayID == "" {
			continue
		}

		displayIDLower := strings.ToLower(displayID)

		// Check if package name contains the search term (substring match)
		nameMatches := strings.Contains(displayIDLower, packageNameLower)

		// Also check aliases
		aliasMatches := false
		for _, alias := range item.Aliases {
			if strings.Contains(strings.ToLower(alias), packageNameLower) {
				aliasMatches = true
				break
			}
		}

		// If either name or alias matches, include this package
		if nameMatches || aliasMatches {
			// Extract provider
			var provider string
			if strings.Contains(sourceID, ":") {
				parts := strings.SplitN(sourceID, ":", 2)
				if len(parts) == 2 {
					provider = parts[0]
				}
			}

			matches = append(matches, PackageMatch{
				SourceID:    sourceID,
				Provider:    provider,
				PackageName: displayID,
				Name:        item.Name,
				Description: item.Description,
				Version:     strings.TrimSpace(item.Version),
			})
		}
	}

	return matches
}

// capitalize capitalizes the first letter of a string
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// promptForProviderSelection prompts the user to select a provider when multiple
// packages with the same name are found across different providers.
// It uses huh confirm for single matches and multi-select for multiple matches.
// This is called when user provided a partial name (without provider prefix),
// in which case confirmation is always shown. When user provides full provider:package-id,
// this function is not called at all.
// action is the verb to use in prompts (e.g., "install", "remove", "update").
// Returns a slice of selected source IDs (can be multiple if multi-select is used).
func promptForProviderSelection(packageName string, matches []PackageMatch, action string) ([]string, error) {
	if len(matches) == 0 {
		return nil, fmt.Errorf("no packages found matching '%s'", packageName)
	}

	// Always show confirmation for partial names
	// This ensures users confirm when they provide partial package names

	// If single match (but fuzzy), show confirm dialog
	if len(matches) == 1 {
		match := matches[0]
		displayName := fmt.Sprintf("%s:%s", match.Provider, match.PackageName)
		if match.Name != "" && match.Name != match.PackageName {
			displayName = fmt.Sprintf("%s (%s)", displayName, match.Name)
		}

		confirm := true // Default to "Yes"
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Found '%s': %s", packageName, displayName)).
					Description(fmt.Sprintf("%s this package? (Press Esc to cancel)", capitalize(action))).
					Affirmative("Yes").
					Negative("No").
					Value(&confirm),
			),
		)

		if err := form.Run(); err != nil {
			// Form was cancelled (e.g., Escape key pressed)
			return nil, fmt.Errorf("user cancelled %s", action)
		}

		if !confirm {
			return nil, fmt.Errorf("user cancelled %s", action)
		}

		return []string{match.SourceID}, nil
	}

	// Multiple matches, show multi-select
	options := make([]huh.Option[string], 0, len(matches))
	for _, match := range matches {
		displayName := fmt.Sprintf("%s:%s", match.Provider, match.PackageName)
		if match.Name != "" && match.Name != match.PackageName {
			displayName = fmt.Sprintf("%s (%s)", displayName, match.Name)
		}
		if match.Description != "" {
			// Truncate description if too long
			desc := match.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			displayName = fmt.Sprintf("%s - %s", displayName, desc)
		}
		options = append(options, huh.NewOption(displayName, match.SourceID))
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(fmt.Sprintf("Found '%s' in multiple providers", packageName)).
				Description(fmt.Sprintf("Select which packages to %s: (Press Esc to cancel)", action)).
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		// Form was cancelled (e.g., Escape key pressed)
		return nil, fmt.Errorf("user cancelled %s", action)
	}

	if len(selected) == 0 {
		// No packages selected (could be Escape or just no selection)
		return nil, fmt.Errorf("user cancelled %s", action)
	}

	return selected, nil
}
