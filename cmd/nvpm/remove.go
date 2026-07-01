package nvpm

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:     "remove <pkgId> [pkgId...]",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove one or more packages",
	Long: `Remove one or more packages from supported providers.

Supported package ID formats:
  npm:@prisma/language-server
  golang:golang.org/x/tools/gopls
  pypi:black
  cargo:ripgrep
  github:user/repo
  gitlab:group/subgroup/project
  codeberg:user/repo

Examples:
  nvpm remove npm:@prisma/language-server
  nvpm rm golang:golang.org/x/tools/gopls npm:eslint
  nvpm delete pypi:black cargo:ripgrep
  nvpm remove npm:prettier golang:golang.org/x/tools/gopls
  nvpm remove github:sharkdp/bat
  nvpm remove gitlab:group/subgroup/myproject`,
	Args: cobra.MinimumNArgs(1),
	// Enable shell completion for installed package IDs only.
	ValidArgsFunction: installedPackageIDCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		userIntegrations := append([]string(nil), removeIntegrations...)
		providers.SetRequestedIntegrations(userIntegrations)

		packages := args

		// Process all packages
		// Use slices instead of fixed-size arrays to handle multi-select results
		internalIDs := make([]string, 0, len(packages))
		displayIDs := make([]string, 0, len(packages))

		for _, userPkgID := range packages {
			// Parse package ID and version from the user-facing ID
			baseID, _ := parsePackageIDAndVersion(userPkgID)

			var internalID string
			var displayID string

			// Check if this is a package name without provider
			if !strings.Contains(baseID, ":") && !strings.HasPrefix(baseID, "pkg:") {
				// Package name without provider - search installed packages and prompt user
				matches := findInstalledPackagesByName(baseID)
				if len(matches) == 0 {
					fmt.Printf("%s No installed packages found matching '%s'\n", IconClose(), baseID)
					return
				}

				// Always show confirmation for partial names (user didn't provide full provider:package-id)
				selectedSourceIDs, err := promptForProviderSelection(baseID, matches, "remove")
				if err != nil {
					fmt.Printf("%s Error selecting provider for '%s': %v\n", IconClose(), baseID, err)
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
					fmt.Printf("Error: %v\n", err)
					return
				}
				if !isSupportedProviderFn(provider) {
					fmt.Printf("Error: Unsupported provider '%s' for package '%s'. Supported providers: %s\n", provider, userPkgID, strings.Join(availableProvidersFn(), ", "))
					return
				}

				internalID = toInternalPackageID(provider, pkgName)
				// Construct displayID from provider and package name (full provider:package-id format)
				displayID = fmt.Sprintf("%s:%s", provider, pkgName)
			}

			internalIDs = append(internalIDs, internalID)
			displayIDs = append(displayIDs, displayID)
		}

		// Remove all packages
		fmt.Printf("Removing %d package(s)...\n", len(internalIDs))

		allSuccess := true
		successCount := 0
		failedCount := 0

		for i := range internalIDs {
			internalID := internalIDs[i]
			displayID := displayIDs[i]

			registryItem := newRegistryParser().GetBySourceId(internalID)
			effectiveIntegrations, resolveErr := providers.ResolveTreeSitterInstallIntegrations(
				registryItem,
				userIntegrations,
				providers.TreeSitterIntegrateResolveOpts{
					MachineOutput: ShouldUseJSONOutput() || ShouldUsePlainOutput(),
				},
			)
			if resolveErr != nil {
				fmt.Printf("%s %v\n", IconClose(), resolveErr)
				failedCount++
				allSuccess = false
				continue
			}
			providers.SetRequestedIntegrations(effectiveIntegrations)

			// Remove the package with spinner showing package name
			var success bool
			action := func() {
				success = removePackageFn(internalID)
			}

			title := fmt.Sprintf("Removing %s...", displayID)
			if err := spinnerutil.Run(title, action); err != nil {
				fmt.Printf("%s Failed to remove %s: %v\n", IconClose(), displayID, err)
				failedCount++
				allSuccess = false
				providers.SetRequestedIntegrations(userIntegrations)
				continue
			}

			providers.SetRequestedIntegrations(userIntegrations)

			if success {
				fmt.Printf("%s Successfully removed %s\n", IconCheck(), displayID)
				successCount++
			} else {
				fmt.Printf("%s Failed to remove %s\n", IconClose(), displayID)
				failedCount++
				allSuccess = false
			}
		}

		// Print summary
		if ShouldUseJSONOutput() {
			result := map[string]interface{}{
				"success_count": successCount,
				"failure_count": failedCount,
				"all_success":   allSuccess,
			}
			PrintJSON(result)
		} else {
			fmt.Printf("\nRemove Summary:\n")
			fmt.Printf("  Successfully removed: %d\n", successCount)
			fmt.Printf("  Failed to remove: %d\n", failedCount)

			if allSuccess {
				fmt.Printf("All packages removed successfully!\n")
			} else {
				fmt.Printf("Some packages failed to remove.\n")
			}
		}
	},
}

var removeIntegrations []string

func init() {
	removeCmd.Flags().StringSliceVar(&removeIntegrations, "integrate", nil, "run integration backends cleanup when removing (e.g. --integrate neovim)")
}

// findInstalledPackagesByName searches installed packages for packages matching the given name
// (substring match, case-insensitive) and returns matches.
// It matches both package names and aliases.
func findInstalledPackagesByName(packageName string) []PackageMatch {
	localPackagesRoot := newLocalPackagesParserFn()
	installedPackages := localPackagesRoot.Packages

	matches := []PackageMatch{}
	packageNameLower := strings.ToLower(packageName)

	// Get registry parser to lookup aliases
	parser := newRegistryParser()

	for _, pkg := range installedPackages {
		sourceID := strings.TrimSpace(pkg.SourceID)
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

		// Also check aliases from registry
		aliasMatches := false
		registryItem := parser.GetBySourceId(sourceID)
		if registryItem.Source.ID != "" {
			for _, alias := range registryItem.Aliases {
				if strings.Contains(strings.ToLower(alias), packageNameLower) {
					aliasMatches = true
					break
				}
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

			// Use registry item name if available, otherwise use displayID
			displayName := displayID
			if registryItem.Source.ID != "" && registryItem.Name != "" {
				displayName = registryItem.Name
			}

			matches = append(matches, PackageMatch{
				SourceID:    sourceID,
				Provider:    provider,
				PackageName: displayID,
				Name:        displayName,
				Description: registryItem.Description,
				Version:     pkg.Version,
			})
		}
	}

	return matches
}

// indirections for testability
var (
	removePackageFn = providers.Remove
)
