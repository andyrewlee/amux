package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// buildCompletionCommand creates the completion command for shell completions.
func buildCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for amux.

To load completions:

Bash:
  $ source <(amux completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ amux completion bash > /etc/bash_completion.d/amux
  # macOS:
  $ amux completion bash > $(brew --prefix)/etc/bash_completion.d/amux

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ amux completion zsh > "${fpath[1]}/_amux"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ amux completion fish | source

  # To load completions for each session, execute once:
  $ amux completion fish > ~/.config/fish/completions/amux.fish

PowerShell:
  PS> amux completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> amux completion powershell > amux.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unknown shell: %s", args[0])
			}
		},
	}

	return cmd
}

// registerCompletions adds custom completions to commands.
func registerCompletions(root *cobra.Command) {
	// Agent name completions
	agentNames := []string{"claude", "codex", "opencode", "amp", "gemini", "droid", "shell"}

	// Find computer run command and add completions
	for _, cmd := range root.Commands() {
		if cmd.Name() == "computer" {
			for _, subCmd := range cmd.Commands() {
				if subCmd.Name() == "run" {
					subCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
						if len(args) == 0 {
							return agentNames, cobra.ShellCompDirectiveNoFileComp
						}
						return nil, cobra.ShellCompDirectiveNoFileComp
					}
				}
				if subCmd.Name() == "update" {
					subCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
						if len(args) == 0 {
							return agentNames[:len(agentNames)-1], cobra.ShellCompDirectiveNoFileComp // Exclude "shell"
						}
						return nil, cobra.ShellCompDirectiveNoFileComp
					}
				}
			}
		}

		// Add completions for explain command
		if cmd.Name() == "explain" {
			cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				if len(args) == 0 {
					topics := []string{"credentials", "sync", "agents", "snapshots", "settings", "architecture"}
					return topics, cobra.ShellCompDirectiveNoFileComp
				}
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
	}

	// Add completions for snapshot commands
	// These would need to fetch from the API, so we'll use dynamic completion
	registerSnapshotCompletions(root)
}

// registerSnapshotCompletions adds snapshot name completions.
func registerSnapshotCompletions(root *cobra.Command) {
	// Find snapshot command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "snapshot" {
			for _, subCmd := range cmd.Commands() {
				if subCmd.Name() == "rm" || subCmd.Name() == "show" {
					subCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
						// Return empty - would need API call to list snapshots
						// This is a placeholder for future implementation
						return nil, cobra.ShellCompDirectiveNoFileComp
					}
				}
			}
		}
	}
}

// FlagCompletionFunc returns completions for flag values.
func FlagCompletionFunc(flagName string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	switch flagName {
	case "credentials":
		return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return []string{"computer", "none", "auto"}, cobra.ShellCompDirectiveNoFileComp
		}
	case "agent":
		return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return []string{"claude", "codex", "opencode", "amp", "gemini", "droid"}, cobra.ShellCompDirectiveNoFileComp
		}
	default:
		return nil
	}
}
