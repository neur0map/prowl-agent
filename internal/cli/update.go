package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/selfupdate"
)

func newUpdateCmd(string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update prowl-agent to the latest build and restart running servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			uiLog.Info("checking for the latest build")
			msg, err := selfupdate.Apply()
			if err != nil {
				return err
			}
			fmt.Fprintln(out, msg)
			// Recycle every running serve/lsp so the agent/editor relaunches the
			// new binary; update replaces the one binary they all share.
			if n := stopServers(findProwlServers("")); n > 0 {
				uiLog.Infof("recycled %d running server(s); your agent or editor relaunches the new binary on next use", n)
			}
			return nil
		},
	}
}
