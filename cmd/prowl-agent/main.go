package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/cli"
)

var version = "v0.14.0"

func main() {
	root := &cobra.Command{
		Use:           "prowl-agent",
		Short:         "Local code index that gives AI agents small, cited answers instead of whole files",
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
