package nvpm

import (
	"fmt"
	"log"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Outputs a script to set environment variables for the current shell",
	Long: `The env command outputs a script that sets environment variables for the current shell.
               This command takes one argument, the shell.
               If omitted, it will default to bash.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 1 {
			log.Fatalln("Too many arguments. The env command takes at most one argument.")
		}
		shell := "bash"
		if len(args) == 1 {
			shell = args[0]
		}
		pathString := files.GetAppBinPath()
		if shell == "pwsh" || shell == "powershell" {
			fmt.Println(`$env:PATH = "` + pathString + `;" + $env:PATH`)
		} else {
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
