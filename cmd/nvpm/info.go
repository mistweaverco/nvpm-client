package nvpm

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Aliases: []string{"show", "details"},
	Use:     "info [package-id]...",
	Short:   "Show detailed information about packages",
	Long: `Show detailed information about one or more packages from the registry.

For Tree-sitter-parser packages, also shows grammar languages, supported integrations,
optional external query repositories, and registry requires (requires.all / requires.one).

Examples:
  nvpm info npm:eslint
  nvpm info pypi:black
  nvpm info golang:golang.org/x/tools/gopls
  nvpm info eslint (will prompt for provider selection if multiple matches)
  nvpm info npm:eslint pypi:black`,
	Args: cobra.MinimumNArgs(1),
	// Enable shell completion for package IDs based on the local registry.
	ValidArgsFunction: packageIDCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		// Ensure registry is available
		_ = downloadAndUnzipRegistryFn()

		parser := newRegistryParser()

		// Process all packages
		packagesToShow := make([]string, 0, len(args))

		for _, userPkgID := range args {
			baseID, _ := parsePackageIDAndVersion(userPkgID)

			// Check if this is a package name without provider
			if !strings.Contains(baseID, ":") && !strings.HasPrefix(baseID, "pkg:") {
				// Package name without provider - search registry and prompt user
				matches := findPackagesByName(baseID)
				if len(matches) == 0 {
					if ShouldUsePlainOutput() {
						fmt.Printf("[✗] No packages found matching '%s'\n", baseID)
					} else {
						fmt.Printf("%s No packages found matching '%s'\n", IconClose(), baseID)
					}
					continue
				}

				// Filter matches to exact package name or alias matches first (for better UX)
				exactMatches := []PackageMatch{}
				partialMatches := []PackageMatch{}
				baseIDLower := strings.ToLower(baseID)
				parserForExactMatch := newRegistryParser()

				for _, match := range matches {
					matchNameLower := strings.ToLower(match.PackageName)
					// Check if package name matches exactly
					isExactMatch := matchNameLower == baseIDLower

					// Also check if any alias matches exactly
					if !isExactMatch {
						registryItem := parserForExactMatch.GetBySourceId(match.SourceID)
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

				// Prompt for selection
				selectedSourceIDs, err := promptForProviderSelection(baseID, matchesToShow, "view")
				if err != nil {
					if ShouldUsePlainOutput() {
						fmt.Printf("[✗] Error selecting provider for '%s': %v\n", baseID, err)
					} else {
						fmt.Printf("%s Error selecting provider for '%s': %v\n", IconClose(), baseID, err)
					}
					continue
				}

				packagesToShow = append(packagesToShow, selectedSourceIDs...)
			} else {
				// Package with provider - parse normally
				provider, pkgName, err := parseUserPackageID(baseID)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					continue
				}
				if !providers.IsSupportedProvider(provider) {
					fmt.Printf("Error: Unsupported provider '%s' for package '%s'. Supported providers: %s\n", provider, userPkgID, strings.Join(providers.AvailableProviders, ", "))
					continue
				}

				sourceID := toInternalPackageID(provider, pkgName)
				packagesToShow = append(packagesToShow, sourceID)
			}
		}

		// Display info for each package
		if ShouldUseJSONOutput() {
			// Collect all packages for JSON output
			packagesInfo := make([]map[string]interface{}, 0, len(packagesToShow))
			for _, sourceID := range packagesToShow {
				item := parser.GetBySourceId(sourceID)
				if item.Source.ID == "" {
					continue
				}
				packagesInfo = append(packagesInfo, buildPackageInfoJSON(item, sourceID))
			}
			if len(packagesInfo) == 1 {
				PrintJSON(packagesInfo[0])
			} else {
				PrintJSON(packagesInfo)
			}
		} else {
			for i, sourceID := range packagesToShow {
				if i > 0 {
					fmt.Println() // Add spacing between multiple packages
				}

				item := parser.GetBySourceId(sourceID)
				if item.Source.ID == "" {
					if ShouldUsePlainOutput() {
						fmt.Printf("[✗] Package '%s' not found in registry\n", sourceID)
					} else {
						fmt.Printf("%s Package '%s' not found in registry\n", IconClose(), sourceID)
					}
					continue
				}

				displayPackageInfo(item, sourceID)
			}
		}
	},
}

// displayPackageInfo renders package information based on output mode
func displayPackageInfo(item registry_parser.RegistryItem, sourceID string) {
	if ShouldUsePlainOutput() {
		displayPackageInfoPlain(item, sourceID)
	} else {
		displayPackageInfoRich(item, sourceID)
	}
}

// displayPackageInfoRich renders package information as markdown using glamour
func displayPackageInfoRich(item registry_parser.RegistryItem, sourceID string) {
	// Build markdown content
	var markdown strings.Builder

	// Package name and ID
	markdown.WriteString(fmt.Sprintf("# %s\n\n", item.Name))
	markdown.WriteString(fmt.Sprintf("**Package ID:** `%s`\n\n", sourceID))

	// Aliases
	if len(item.Aliases) > 0 {
		markdown.WriteString(fmt.Sprintf("**Aliases:** %s\n\n", strings.Join(item.Aliases, ", ")))
	}

	// Version
	if item.Version != "" {
		markdown.WriteString(fmt.Sprintf("**Version:** `%s`\n\n", item.Version))
	}

	// Description
	if item.Description != "" {
		markdown.WriteString(fmt.Sprintf("## Description\n\n%s\n\n", item.Description))
	}

	// Homepage
	if item.Homepage != "" {
		markdown.WriteString(fmt.Sprintf("**Homepage:** %s\n\n", item.Homepage))
	}

	// Provider
	if strings.Contains(sourceID, ":") {
		parts := strings.SplitN(sourceID, ":", 2)
		if len(parts) == 2 {
			markdown.WriteString(fmt.Sprintf("**Provider:** `%s`\n\n", parts[0]))
		}
	}

	// Licenses
	if len(item.Licenses) > 0 {
		markdown.WriteString(fmt.Sprintf("**Licenses:** %s\n\n", strings.Join(item.Licenses, ", ")))
	}

	// Languages
	if len(item.Languages) > 0 {
		markdown.WriteString(fmt.Sprintf("**Languages:** %s\n\n", strings.Join(item.Languages, ", ")))
	}

	// Categories
	if len(item.Categories) > 0 {
		markdown.WriteString(fmt.Sprintf("**Categories:** %s\n\n", strings.Join(item.Categories, ", ")))
	}

	// Installation status
	localPackagesRoot := newLocalPackagesParserFn()
	installedPackages := localPackagesRoot.Packages
	isInstalled := false
	installedVersion := ""
	for _, pkg := range installedPackages {
		if pkg.SourceID == sourceID {
			isInstalled = true
			installedVersion = pkg.Version
			break
		}
	}

	if isInstalled {
		if installedVersion != "" {
			markdown.WriteString(fmt.Sprintf("**Status:** ✅ Installed (version: `%s`)\n\n", installedVersion))
		} else {
			markdown.WriteString("**Status:** ✅ Installed\n\n")
		}
	} else {
		markdown.WriteString("**Status:** ⬜ Not installed\n\n")
	}

	// Binaries
	if len(item.Bin) > 0 {
		markdown.WriteString("## Binaries\n\n")
		for binName, binPath := range item.Bin {
			markdown.WriteString(fmt.Sprintf("- **%s:** `%s`\n", binName, binPath))
		}
		markdown.WriteString("\n")
	}

	extra := collectPackageExtraDetails(item)
	if extra.Requires != nil {
		appendRequiresMarkdown(&markdown, extra.Requires)
	}
	if extra.TreeSitter != nil {
		appendTreeSitterMarkdown(&markdown, extra.TreeSitter)
	}

	// Render markdown with glamour
	rendered, err := glamour.Render(markdown.String(), "dark")
	if err != nil {
		// Fallback to plain text if rendering fails
		fmt.Println(markdown.String())
		return
	}

	fmt.Print(rendered)
}

// displayPackageInfoPlain renders package information as plain text
func displayPackageInfoPlain(item registry_parser.RegistryItem, sourceID string) {
	fmt.Printf("Name: %s\n", item.Name)
	fmt.Printf("Package ID: %s\n", sourceID)

	if len(item.Aliases) > 0 {
		fmt.Printf("Aliases: %s\n", strings.Join(item.Aliases, ", "))
	}

	if item.Version != "" {
		fmt.Printf("Version: %s\n", item.Version)
	}

	if item.Description != "" {
		fmt.Printf("Description: %s\n", item.Description)
	}

	if item.Homepage != "" {
		fmt.Printf("Homepage: %s\n", item.Homepage)
	}

	if strings.Contains(sourceID, ":") {
		parts := strings.SplitN(sourceID, ":", 2)
		if len(parts) == 2 {
			fmt.Printf("Provider: %s\n", parts[0])
		}
	}

	if len(item.Licenses) > 0 {
		fmt.Printf("Licenses: %s\n", strings.Join(item.Licenses, ", "))
	}

	if len(item.Languages) > 0 {
		fmt.Printf("Languages: %s\n", strings.Join(item.Languages, ", "))
	}

	if len(item.Categories) > 0 {
		fmt.Printf("Categories: %s\n", strings.Join(item.Categories, ", "))
	}

	// Installation status
	localPackagesRoot := newLocalPackagesParserFn()
	installedPackages := localPackagesRoot.Packages
	isInstalled := false
	installedVersion := ""
	for _, pkg := range installedPackages {
		if pkg.SourceID == sourceID {
			isInstalled = true
			installedVersion = pkg.Version
			break
		}
	}

	if isInstalled {
		if installedVersion != "" {
			fmt.Printf("Status: Installed (version: %s)\n", installedVersion)
		} else {
			fmt.Printf("Status: Installed\n")
		}
	} else {
		fmt.Printf("Status: Not installed\n")
	}

	if len(item.Bin) > 0 {
		fmt.Printf("Binaries:\n")
		for binName, binPath := range item.Bin {
			fmt.Printf("  %s: %s\n", binName, binPath)
		}
	}

	extra := collectPackageExtraDetails(item)
	if extra.Requires != nil {
		var b strings.Builder
		appendRequiresPlain(&b, extra.Requires)
		fmt.Print(b.String())
	}
	if extra.TreeSitter != nil {
		var b strings.Builder
		appendTreeSitterPlain(&b, extra.TreeSitter)
		fmt.Print(b.String())
	}
}

// buildPackageInfoJSON builds a JSON representation of package info
func buildPackageInfoJSON(item registry_parser.RegistryItem, sourceID string) map[string]interface{} {
	result := make(map[string]interface{})
	result["name"] = item.Name
	result["package_id"] = sourceID

	if len(item.Aliases) > 0 {
		result["aliases"] = item.Aliases
	}

	if item.Version != "" {
		result["version"] = item.Version
	}

	if item.Description != "" {
		result["description"] = item.Description
	}

	if item.Homepage != "" {
		result["homepage"] = item.Homepage
	}

	if strings.Contains(sourceID, ":") {
		parts := strings.SplitN(sourceID, ":", 2)
		if len(parts) == 2 {
			result["provider"] = parts[0]
		}
	}

	if len(item.Licenses) > 0 {
		result["licenses"] = item.Licenses
	}

	if len(item.Languages) > 0 {
		result["languages"] = item.Languages
	}

	if len(item.Categories) > 0 {
		result["categories"] = item.Categories
	}

	// Installation status
	localPackagesRoot := newLocalPackagesParserFn()
	installedPackages := localPackagesRoot.Packages
	isInstalled := false
	installedVersion := ""
	for _, pkg := range installedPackages {
		if pkg.SourceID == sourceID {
			isInstalled = true
			installedVersion = pkg.Version
			break
		}
	}

	status := "not_installed"
	if isInstalled {
		status = "installed"
		if installedVersion != "" {
			result["installed_version"] = installedVersion
		}
	}
	result["status"] = status

	if len(item.Bin) > 0 {
		result["binaries"] = item.Bin
	}

	mergeExtraDetailsJSON(result, collectPackageExtraDetails(item))

	return result
}
