package cli

import (
	"github.com/spf13/cobra"
)

// Verbose controls whether verbose output is enabled.
var Verbose bool

func buildSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandboxes",
	}
	cmd.AddCommand(buildSandboxRunCommand())
	cmd.AddCommand(buildSandboxUpdateCommand())
	cmd.AddCommand(buildSandboxPreviewCommand())
	cmd.AddCommand(buildSandboxLogsCommand())
	cmd.AddCommand(buildSandboxDesktopCommand())
	cmd.AddCommand(buildSandboxLsCommand())
	cmd.AddCommand(buildSandboxRmCommand())
	cmd.AddCommand(buildSandboxResetCommand())
	return cmd
}
