package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/selfupdate"
)

func newUpdateCmd(string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update prowl-agent to the latest published build",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Checking for the latest build ...")
			msg, err := selfupdate.Apply()
			if err != nil {
				return err
			}
			fmt.Fprintln(out, msg)
			return nil
		},
	}
}
