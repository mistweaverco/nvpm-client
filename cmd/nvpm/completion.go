package nvpm

import (
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/spf13/cobra"
)

// displayPackageNameFromRegistryID converts an internal registry/source ID into
// the user-facing format used on the CLI.
//
// Internal IDs are currently of the form:
//
//	<provider>:<package-id>
//
// and are exposed to users as:
//
//	<provider>:<package-id>
func displayPackageNameFromRegistryID(sourceID string) string {
	if sourceID == "" {
		return ""
	}
	packageName := sourceID
	if strings.Contains(sourceID, ":") {
		provider_and_package_name := strings.SplitN(sourceID, ":", 2)
		if len(provider_and_package_name) == 0 {
			return ""
		}
		packageName = provider_and_package_name[1]
	}
	return packageName
}

// newRegistryParser is an indirection for tests.
var newRegistryParser = registry_parser.NewDefaultRegistryParser

// packageIDCompletion provides shell completion for package IDs based on the
// locally available registry data. It matches package names (without provider prefix)
// using substring matching (case-insensitive), allowing users to search by package name
// without needing to know the provider.
//
// When the user types without a provider prefix (e.g., "yaml"), it matches package names
// that contain the typed text and returns the full "provider:package" format.
// When the user types with a provider prefix (e.g., "npm:yaml"), it matches the full ID.
func packageIDCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	parser := newRegistryParser()
	items := parser.GetData(false)

	completions := make([]string, 0, len(items))
	toCompleteLower := strings.ToLower(toComplete)

	// Check if user has started typing a provider prefix (contains colon)
	hasProviderPrefix := strings.Contains(toComplete, ":")

	if hasProviderPrefix {
		// User is typing with provider prefix, match on the full ID prefix
		for _, item := range items {
			fullID := strings.TrimSpace(item.Source.ID)
			if fullID == "" {
				continue
			}
			// Match if the full ID starts with what the user typed (case-insensitive)
			if toComplete == "" || strings.HasPrefix(strings.ToLower(fullID), toCompleteLower) {
				completions = append(completions, fullID)
			}
		}
	} else {
		// User is typing without provider prefix, match on package name and aliases
		//
		// IMPORTANT: Shell completion filters returned strings by prefix.
		// When user types "yaml", the shell filters out "npm:yaml-language-server"
		// because it doesn't start with "yaml".
		//
		// Solution: Return package names WITHOUT provider prefix when no provider
		// is specified. The install command will detect missing provider and search.
		// This allows substring matching to work in completions.
		for _, item := range items {
			displayID := displayPackageNameFromRegistryID(strings.TrimSpace(item.Source.ID))
			if displayID == "" {
				continue
			}
			displayIDLower := strings.ToLower(displayID)

			// Match if package name contains the typed text (substring match, case-insensitive)
			nameMatches := toComplete == "" || strings.Contains(displayIDLower, toCompleteLower)

			// Also check aliases
			aliasMatches := false
			for _, alias := range item.Aliases {
				if toComplete == "" || strings.Contains(strings.ToLower(alias), toCompleteLower) {
					aliasMatches = true
					break
				}
			}

			// If either name or alias matches, include in completions
			if nameMatches || aliasMatches {
				// Return package name without provider - install command will handle provider selection
				completions = append(completions, displayID)
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// newLocalPackagesParserFn is an indirection for tests.
var newLocalPackagesParserFn = func() local_packages_parser.LocalPackageRoot {
	return local_packages_parser.GetData(false)
}

// installedPackageIDCompletion provides shell completion for installed package IDs only.
// It matches package names (without provider prefix) using substring matching (case-insensitive),
// allowing users to search by package name without needing to know the provider.
//
// When the user types without a provider prefix (e.g., "yaml"), it matches package names
// that contain the typed text and returns just the package name.
// When the user types with a provider prefix (e.g., "npm:yaml"), it matches the full ID.
func installedPackageIDCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	localPackagesRoot := newLocalPackagesParserFn()
	installedPackages := localPackagesRoot.Packages

	completions := make([]string, 0, len(installedPackages))
	toCompleteLower := strings.ToLower(toComplete)

	// Check if user has started typing a provider prefix (contains colon)
	hasProviderPrefix := strings.Contains(toComplete, ":")

	if hasProviderPrefix {
		// User is typing with provider prefix, match on the full ID prefix
		for _, pkg := range installedPackages {
			sourceID := strings.TrimSpace(pkg.SourceID)
			if sourceID == "" {
				continue
			}
			// Match if the full ID starts with what the user typed (case-insensitive)
			if toComplete == "" || strings.HasPrefix(strings.ToLower(sourceID), toCompleteLower) {
				completions = append(completions, sourceID)
			}
		}
	} else {
		// User is typing without provider prefix, match on package name and aliases
		// Return package names WITHOUT provider prefix so shell completion works
		parser := newRegistryParser()
		for _, pkg := range installedPackages {
			displayID := displayPackageNameFromRegistryID(strings.TrimSpace(pkg.SourceID))
			if displayID == "" {
				continue
			}
			displayIDLower := strings.ToLower(displayID)

			// Match if package name contains the typed text (substring match, case-insensitive)
			nameMatches := toComplete == "" || strings.Contains(displayIDLower, toCompleteLower)

			// Also check aliases from registry
			aliasMatches := false
			sourceID := strings.TrimSpace(pkg.SourceID)
			if sourceID != "" {
				registryItem := parser.GetBySourceId(sourceID)
				if registryItem.Source.ID != "" {
					for _, alias := range registryItem.Aliases {
						if toComplete == "" || strings.Contains(strings.ToLower(alias), toCompleteLower) {
							aliasMatches = true
							break
						}
					}
				}
			}

			// If either name or alias matches, include in completions
			if nameMatches || aliasMatches {
				// Return package name without provider - remove/update commands will handle provider selection
				completions = append(completions, displayID)
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}
