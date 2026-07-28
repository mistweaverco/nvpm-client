package nvpm

import (
	"fmt"
	"log"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env [shell]",
	Short: "Outputs a script to set environment variables for the current shell",
	Long: `The env command outputs a script that sets environment variables for the current shell.
               This command takes one argument, the shell.
               Supported shells: bash, zsh, fish, pwsh, powershell.
               If omitted, it will default to bash.`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "pwsh", "powershell"},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 1 {
			log.Fatalln("Too many arguments. The env command takes at most one argument.")
		}
		shell := "bash"
		if len(args) == 1 {
			shell = args[0]
		}
		pathString := files.GetAppBinPath()
		switch shell {
		case "pwsh", "powershell":
			fmt.Println(`$env:PATH = "` + pathString + `;" + $env:PATH`)
		case "fish":
			fmt.Println(`# nvpm shell setup; adapted from rustup
if not contains -- "` + pathString + `" $PATH
    # Prepending path in case a system-installed nvpm executable needs to be overridden
    set -x PATH "` + pathString + `" $PATH
end`)
		default:
			// bash, zsh, and other POSIX-compatible shells
			fmt.Println(`#!/bin/sh
# nvpm shell setup; adapted from rustup
# affix colons on either side of $PATH to simplify matching
case ":${PATH}:" in
    *:"` + pathString + `":*)
        ;;
    *)
        # Prepending path in case a system-installed nvpm executable needs to be overridden
        export PATH="` + pathString + `:$PATH"
        ;;
esac`)
		}
	},
}