package nvpm

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check system health and requirements",
	Long: `Check if the system meets all requirements for running NVPM.

This command verifies the presence of required tools and dependencies for all providers.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Check all providers
		providerStatuses := checkAllProvidersHealthFn()

		if ShouldUseJSONOutput() {
			result := map[string]interface{}{
				"providers": providerStatuses,
			}
			PrintJSON(result)
		} else {
			if !ShouldUsePlainOutput() {
				fmt.Printf("%s Checking provider health...\n", IconMagnify())
				fmt.Println()
			}

			// Display all providers
			hasWarnings := false
			for _, status := range providerStatuses {
				icon := getProviderIcon(status.Provider)
				if status.Available {
					fmt.Printf("%s %s: Available\n", icon, strings.ToUpper(status.Provider))
				} else {
					hasWarnings = true
					fmt.Printf("%s %s: %s Not available (missing: %s)\n", icon, strings.ToUpper(status.Provider), IconAlert(), status.RequiredTool)
					fmt.Printf("   %s\n", status.Description)
				}
				fmt.Println()
			}

			// Overall status
			if !hasWarnings {
				fmt.Printf("%s All providers are available! Your system is ready to use NVPM.\n", IconCheckCircle())
			} else {
				fmt.Printf("%s Some providers are not available. Install the required tools to use those providers.\n", IconAlert())
			}
		}
	},
}

// indirection for testability
var checkAllProvidersHealthFn = providers.CheckAllProvidersHealth
