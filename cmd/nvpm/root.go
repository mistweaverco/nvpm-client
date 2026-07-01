package nvpm

import (
	"fmt"
	"os"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/version"
	"github.com/spf13/cobra"
)

var cfg = config.NewConfig(config.Config{
	Flags: config.ConfigFlags{
		CacheMaxAge: 24 * time.Hour,        // Default to 24 hours
		Color:       config.ColorModeAuto,  // Default to auto (respect TTY)
		Output:      config.OutputModeRich, // Default to rich output
	},
})

var rootCmd = &cobra.Command{
	Use:   "nvpm",
	Short: "NVPM is Mason.nvim, but not only for Neovim",
	Long:  "NVPM is a minimal CLI for managing LSP servers, DAP servers, linters, and formatters, for Neovim, but not limited to just Neovim.",
	Run: func(cmd *cobra.Command, files []string) {
		if cfg.Flags.Version {
			fmt.Println(version.VERSION)
			return
		} else {
			// Show help if no command provided
			cmd.Help()
		}
	},
}

func Execute() {
	// Parse flags first to get color config
	err := rootCmd.Execute()
	if err != nil {
		osExit(1)
	}
}

func init() {
	rootCmd.AddCommand(envCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.PersistentFlags().BoolVar(&cfg.Flags.Version, "version", false, "version")
	rootCmd.PersistentFlags().DurationVar(&cfg.Flags.CacheMaxAge, "cache-max-age", 24*time.Hour, "maximum age of registry cache (e.g., 1h, 24h, 7d)")
	colorFlag := rootCmd.PersistentFlags().VarPF(&cfg.Flags.Color, "color", "", "when to use colors and icons: always, auto (default), never")
	colorFlag.NoOptDefVal = string(config.ColorModeAlways) // If --color is used without value, default to "always"

	// Use StringVarP for output flag so it properly consumes the next argument as value
	var outputFlagValue string
	rootCmd.PersistentFlags().StringVarP(&outputFlagValue, "output", "o", string(config.OutputModeRich), "output format: rich (default), plain, json")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Load optional config.yaml (next to nvpm-lock.json) and apply defaults
		// only when the user didn't explicitly set flags.
		if fileCfg, ok, err := config.LoadFileConfig(); err == nil && ok {
			if !cmd.Flags().Changed("cache-max-age") {
				if d := fileCfg.RegistryCacheMaxAgeOrZero(); d > 0 {
					cfg.Flags.CacheMaxAge = d
				}
			}
			if !cmd.Flags().Changed("color") && fileCfg.UI.Color != "" {
				_ = cfg.Flags.Color.Set(fileCfg.UI.Color) // ignore invalid values, keep defaults
			}
			if !cmd.Flags().Changed("output") && fileCfg.UI.Output != "" {
				outputFlagValue = fileCfg.UI.Output
			}
		}

		// Parse output mode from flag value
		if outputFlagValue != "" {
			var outputMode config.OutputMode
			if err := outputMode.Set(outputFlagValue); err != nil {
				// Invalid value, use default
				cfg.Flags.Output = config.OutputModeRich
			} else {
				cfg.Flags.Output = outputMode
			}
		}
	}

	// Set up the color config accessor for icons.go
	SetColorConfigFunc(func() config.ConfigFlags {
		return cfg.Flags
	})
}

// osExit is a variable to allow overriding in tests
var osExit = os.Exit
