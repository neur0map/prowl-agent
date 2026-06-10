package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/cli"
)

var version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "prowl-agent",
		Short:         "Local code and config intelligence for AI coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	cli.Register(root, version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
