package nvpm

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync registry or packages",
	Long: `Sync registry or packages.

The sync command has two subcommands:
  registry  - Download and unzip the latest registry file
  packages  - Ensure all packages in nvpm-lock.json are installed in exact versions`,
}

var syncRegistryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Download and unzip the latest registry file",
	Long: `Download and unzip the latest registry file from the registry URL.

This command downloads the registry file and extracts it to the app data directory.
The registry URL list can be overridden using the NVPM_REGISTRY_URLS environment variable (comma/space-separated).`,
	Run: func(cmd *cobra.Command, args []string) {
		if !ShouldUseJSONOutput() && !ShouldUsePlainOutput() {
			fmt.Println("Downloading registry...")
		}
		if err := syncRegistryFn(); err != nil {
			if ShouldUseJSONOutput() {
				result := map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				}
				PrintJSON(result)
			} else {
				fmt.Printf("%s Failed to sync registry: %v\n", IconClose(), err)
			}
			osExit(1)
			return
		}
		if ShouldUseJSONOutput() {
			result := map[string]interface{}{
				"success": true,
			}
			PrintJSON(result)
		} else {
			fmt.Printf("%s Registry synced successfully\n", IconCheck())
		}
	},
}

var syncPackagesCmd = &cobra.Command{
	Use:   "packages",
	Short: "Sync all packages from nvpm-lock.json",
	Long: `Ensure all packages defined in nvpm-lock.json are installed in the exact versions specified.

This command reads the nvpm-lock.json file and ensures that all packages
are installed with their exact versions as specified in the lock file.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := providers.ConfigureExternalTreeSitterQueriesFromCLI(
			cmd.Flags().Changed("external-treesitter-queries"),
			syncExternalTreeSitterQueries,
		); err != nil {
			fmt.Printf("%s Invalid --external-treesitter-queries: %v\n", IconClose(), err)
			osExit(1)
			return
		}
		if !ShouldUseJSONOutput() && !ShouldUsePlainOutput() {
			fmt.Println("Syncing packages from nvpm-lock.json...")

			cleanupNestedInstallOutput := registerNestedInstallOutputHooks()
			defer cleanupNestedInstallOutput()

			lock := local_packages_parser.GetData(false)
			if len(lock.Packages) == 0 {
				fmt.Println("  (no packages in lockfile)")
				fmt.Printf("%s Packages sync completed\n", IconCheck())
				return
			}

			if err := providers.EnsureLockfilePackageRequires(false); err != nil {
				fmt.Printf("%s %v\n", IconClose(), err)
				osExit(1)
				return
			}
			providers.ResetTreeSitterDependencyInstallSuccessCount()
			lock = local_packages_parser.GetData(false)

			type pkgResult struct {
				id                string
				version           string
				ok                bool
				integrations      []string
				integrationReport []string
			}

			results := make([]pkgResult, 0, len(lock.Packages))
			successCount := 0
			failureCount := 0

			for _, pkg := range lock.Packages {
				id := strings.TrimSpace(pkg.SourceID)
				ver := strings.TrimSpace(pkg.Version)
				if id == "" || ver == "" {
					continue
				}

				var ints []string
				if pkg.Extras != nil {
					ints = pkg.Extras.Integrations
				}
				providers.SetRequestedIntegrations(ints)

				registryItem := newRegistryParser().GetBySourceId(id)

				title := fmt.Sprintf("Syncing %s@%s", id, ver)
				if len(ints) > 0 {
					title = fmt.Sprintf("Syncing %s@%s (integrations: %v)", id, ver, ints)
				}

				ok, err := runNvpmInstallWithTreeSitterSpinnerPhases(title, id, ver, registryItem, func() bool {
					return providers.Install(id, ver)
				})
				if err != nil {
					failureCount++
					fmt.Printf("%s Failed to sync %s@%s: %v\n", IconClose(), id, ver, err)
					continue
				}

				res := pkgResult{
					id:           id,
					version:      ver,
					ok:           ok,
					integrations: ints,
				}
				res.integrationReport = providers.ConsumeIntegrationReport(id, ver)
				results = append(results, res)

				if ok {
					successCount++
					fmt.Printf("%s Synced %s@%s\n", IconCheck(), id, ver)
					for _, line := range res.integrationReport {
						fmt.Printf("  %s@%s: %s\n", id, ver, line)
					}
				} else {
					failureCount++
					fmt.Printf("%s Failed to sync %s@%s\n", IconClose(), id, ver)
				}
			}

			// Final overview.
			fmt.Printf("\nSync Summary:\n")
			fmt.Printf("  Successfully synced: %d\n", successCount)
			if failureCount > 0 {
				fmt.Printf("  Failed to sync: %d\n", failureCount)
			}
			fmt.Printf("%s Packages sync completed\n", IconCheck())
			return
		}

		// Plain/JSON output keeps the old all-at-once behavior for scripting.
		if err := syncPackagesFn(); err != nil {
			if ShouldUseJSONOutput() {
				result := map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				}
				PrintJSON(result)
			} else {
				fmt.Printf("%s Failed to sync packages: %v\n", IconClose(), err)
			}
			osExit(1)
			return
		}

		if ShouldUseJSONOutput() {
			result := map[string]interface{}{
				"success": true,
			}
			PrintJSON(result)
		} else {
			fmt.Printf("%s Packages sync completed\n", IconCheck())
		}
	},
}

var syncExternalTreeSitterQueries string

func init() {
	syncCmd.AddCommand(syncRegistryCmd)
	syncCmd.AddCommand(syncPackagesCmd)
	syncPackagesCmd.Flags().StringVar(&syncExternalTreeSitterQueries, "external-treesitter-queries", "ask", "optional Neovim query-only git clones: ask, always, never (NVPM_EXTERNAL_TREESITTER_QUERIES when default)")
}

// downloadAndUnzipRegistryForced downloads and unzips the registry, forcing a fresh download
func downloadAndUnzipRegistryForced() error {
	return files.DownloadAndUnzipRegistryForced()
}

// indirections for testability
var (
	syncRegistryFn = downloadAndUnzipRegistryForced
	syncPackagesFn = providers.SyncAllFromLock
)
