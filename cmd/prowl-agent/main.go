package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "prowl-agent",
		Short:         "Local-first ricing/dotfiles config-intelligence backend",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	// subcommands wired in later phases: init, status, serve
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
