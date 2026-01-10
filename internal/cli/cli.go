package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Run executes the AMUX CLI. It returns a process exit code.
func Run(args []string) int {
	root := buildRootCommand()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if exitErr, ok := err.(exitError); ok {
			return exitErr.code
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func buildRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "amux",
		Short:         "Daytona-powered sandbox CLI for Claude Code, Codex, OpenCode, Amp, Gemini, and Droid",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.Version = "0.1.0"
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(buildSetupCommand())
	root.AddCommand(buildDoctorCommand())
	root.AddCommand(buildSnapshotCommand())
	root.AddCommand(buildAuthCommand())
	root.AddCommand(buildSandboxCommand())

	return root
}
