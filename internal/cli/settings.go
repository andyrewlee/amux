package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildSettingsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage local settings sync to sandboxes",
	}

	cmd.AddCommand(buildSettingsSyncCommand())
	cmd.AddCommand(buildSettingsStatusCommand())

	return cmd
}

func buildSettingsSyncCommand() *cobra.Command {
	var enable bool
	var disable bool
	var claude bool
	var codex bool
	var git bool
	var all bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Configure which local settings to sync to sandboxes",
		Long: `Configure which local settings files to sync to cloud sandboxes.

Settings sync is opt-in and requires explicit consent. When enabled, amux will
copy your local configuration files (like ~/.claude/settings.json) to the
persistent credentials volume so your preferences are available in sandboxes.

IMPORTANT: Settings sync only copies non-sensitive configuration. API keys,
tokens, and credentials are automatically filtered out.

Examples:
  amux settings sync --enable --claude    # Enable Claude settings sync
  amux settings sync --enable --all       # Enable all settings sync
  amux settings sync --disable            # Disable all settings sync
  amux settings sync                      # Show current sync status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}

			syncCfg := cfg.SettingsSync

			// Handle disable flag
			if disable {
				syncCfg.Enabled = false
				syncCfg.Claude = false
				syncCfg.Codex = false
				syncCfg.Git = false
				syncCfg.Shell = false
				cfg.SettingsSync = syncCfg
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Println("Settings sync disabled")
				return nil
			}

			// Handle enable flag
			if enable {
				syncCfg.Enabled = true

				if all {
					syncCfg.Claude = true
					syncCfg.Codex = true
					syncCfg.Git = true
				} else {
					if claude {
						syncCfg.Claude = true
					}
					if codex {
						syncCfg.Codex = true
					}
					if git {
						syncCfg.Git = true
					}
				}

				// Show consent notice
				fmt.Println("amux settings sync")
				fmt.Println(strings.Repeat("─", 50))
				fmt.Println()
				fmt.Println("You are enabling settings sync. This will copy the following")
				fmt.Println("local configuration files to your sandbox credentials volume:")
				fmt.Println()

				if syncCfg.Claude {
					fmt.Println("  • ~/.claude/settings.json (Claude Code preferences)")
				}
				if syncCfg.Codex {
					fmt.Println("  • ~/.codex/config.toml (Codex preferences)")
				}
				if syncCfg.Git {
					fmt.Println("  • ~/.gitconfig (name, email, aliases - NO credentials)")
				}

				fmt.Println()
				fmt.Println("Note: API keys and tokens are automatically filtered out.")
				fmt.Println()

				cfg.SettingsSync = syncCfg
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}

				fmt.Println("✓ Settings sync enabled")
				fmt.Println()
				fmt.Println("Your settings will be synced on next sandbox start.")
				return nil
			}

			// Show current status if no flags
			return showSettingsSyncStatus(syncCfg)
		},
	}

	cmd.Flags().BoolVar(&enable, "enable", false, "Enable settings sync")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable settings sync")
	cmd.Flags().BoolVar(&claude, "claude", false, "Sync Claude settings")
	cmd.Flags().BoolVar(&codex, "codex", false, "Sync Codex settings")
	cmd.Flags().BoolVar(&git, "git", false, "Sync git config (safe keys only)")
	cmd.Flags().BoolVar(&all, "all", false, "Sync all supported settings")

	return cmd
}

func buildSettingsStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local settings files and sync status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}

			fmt.Println("amux settings status")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Println()

			// Show sync status
			syncCfg := cfg.SettingsSync
			if syncCfg.Enabled {
				fmt.Println("Settings sync: enabled")
			} else {
				fmt.Println("Settings sync: disabled")
			}
			fmt.Println()

			// Show local settings files
			fmt.Println("Local settings files:")
			localStatus := sandbox.GetLocalSettingsStatus()

			for _, setting := range sandbox.KnownAgentSettings() {
				exists := localStatus[setting.Agent]
				syncEnabled := false

				switch setting.Agent {
				case sandbox.AgentClaude:
					syncEnabled = syncCfg.Claude
				case sandbox.AgentCodex:
					syncEnabled = syncCfg.Codex
				}

				status := "not found"
				if exists {
					if syncEnabled && syncCfg.Enabled {
						status = "found (syncing)"
					} else {
						status = "found"
					}
				}

				fmt.Printf("  %s: %s\n", setting.Agent, status)
				fmt.Printf("    %s\n", setting.Description)
			}

			// Git config status
			gitExists := localStatus["git"]
			gitStatus := "not found"
			if gitExists {
				if syncCfg.Git && syncCfg.Enabled {
					gitStatus = "found (syncing safe keys)"
				} else {
					gitStatus = "found"
				}
			}
			fmt.Printf("  git: %s\n", gitStatus)
			fmt.Println("    Git configuration (name, email, aliases)")

			fmt.Println()
			fmt.Println(strings.Repeat("─", 50))

			if !syncCfg.Enabled {
				fmt.Println("Run `amux settings sync --enable --all` to sync settings")
			}

			return nil
		},
	}

	return cmd
}

func showSettingsSyncStatus(cfg sandbox.SettingsSyncConfig) error {
	fmt.Println("amux settings sync status")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	if cfg.Enabled {
		fmt.Println("Settings sync: enabled")
		fmt.Println()
		fmt.Println("Syncing:")
		if cfg.Claude {
			fmt.Println("  ✓ Claude settings")
		}
		if cfg.Codex {
			fmt.Println("  ✓ Codex settings")
		}
		if cfg.Git {
			fmt.Println("  ✓ Git config (safe keys)")
		}
		if cfg.Shell {
			fmt.Println("  ✓ Shell preferences")
		}
		if !cfg.Claude && !cfg.Codex && !cfg.Git && !cfg.Shell {
			fmt.Println("  (no settings selected)")
		}
	} else {
		fmt.Println("Settings sync: disabled")
		fmt.Println()
		fmt.Println("Enable with: amux settings sync --enable [--claude] [--codex] [--git] [--all]")
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	return nil
}
