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
	cmd.AddCommand(buildSettingsShowCommand())

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
sandbox so your preferences are available in cloud sessions.

IMPORTANT: Settings sync only copies non-sensitive configuration. API keys,
tokens, and credentials are automatically filtered out.

Examples:
  amux settings sync --enable --claude    # Enable Claude settings sync
  amux settings sync --enable --all       # Enable all detected settings
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
				syncCfg.Files = nil
				syncCfg.Claude = false
				syncCfg.Codex = false
				syncCfg.Git = false
				syncCfg.Shell = false
				cfg.SettingsSync = syncCfg
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Fprintln(cliStdout, "Settings sync disabled")
				return nil
			}

			// Handle enable flag
			if enable {
				// Detect existing settings files
				detected := sandbox.DetectExistingSettings()

				fmt.Fprintln(cliStdout, "amux settings sync")
				fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
				fmt.Fprintln(cliStdout)

				// Show detected files
				if len(detected) > 0 {
					fmt.Fprintln(cliStdout, "Detected settings files:")
					for _, s := range detected {
						fmt.Fprintf(cliStdout, "  ~/%s (%s)\n", s.HomePath, s.Description)
					}
					fmt.Fprintln(cliStdout)
				} else {
					fmt.Fprintln(cliStdout, "No settings files detected locally.")
					fmt.Fprintln(cliStdout)
					return nil
				}

				// Determine which files to sync based on flags
				var filesToSync []string

				if all {
					// Sync all detected files
					for _, s := range detected {
						filesToSync = append(filesToSync, "~/"+s.HomePath)
					}
				} else {
					// Sync only specified agents
					for _, s := range detected {
						shouldSync := false
						switch s.Agent {
						case "claude":
							shouldSync = claude
						case "codex":
							shouldSync = codex
						case "git":
							shouldSync = git
						}
						if shouldSync {
							filesToSync = append(filesToSync, "~/"+s.HomePath)
						}
					}

					// If no specific flags given, show help
					if len(filesToSync) == 0 && !claude && !codex && !git {
						fmt.Fprintln(cliStdout, "Specify which settings to sync:")
						fmt.Fprintln(cliStdout, "  --all      Sync all detected files")
						fmt.Fprintln(cliStdout, "  --claude   Sync Claude settings")
						fmt.Fprintln(cliStdout, "  --codex    Sync Codex settings")
						fmt.Fprintln(cliStdout, "  --git      Sync git config")
						fmt.Fprintln(cliStdout)
						fmt.Fprintln(cliStdout, "Example: amux settings sync --enable --all")
						return nil
					}
				}

				if len(filesToSync) == 0 {
					fmt.Fprintln(cliStdout, "No matching settings files to sync.")
					return nil
				}

				// Show what will be synced
				fmt.Fprintln(cliStdout, "Will sync these files to sandbox:")
				for _, f := range filesToSync {
					note := ""
					if strings.Contains(f, ".gitconfig") {
						note = " (safe keys only)"
					}
					fmt.Fprintf(cliStdout, "  %s%s\n", f, note)
				}
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Note: API keys and tokens are automatically filtered out.")
				fmt.Fprintln(cliStdout)

				// Save config with explicit file list
				syncCfg.Enabled = true
				syncCfg.Files = filesToSync
				// Also set legacy flags for backwards compatibility
				for _, f := range filesToSync {
					if strings.Contains(f, ".claude") {
						syncCfg.Claude = true
					}
					if strings.Contains(f, ".codex") {
						syncCfg.Codex = true
					}
					if strings.Contains(f, ".gitconfig") {
						syncCfg.Git = true
					}
				}

				cfg.SettingsSync = syncCfg
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}

				fmt.Fprintln(cliStdout, "✓ Settings sync enabled")
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Files will sync on next `amux claude/codex/...` run.")
				fmt.Fprintln(cliStdout, "To disable: amux settings sync --disable")
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
	cmd.Flags().BoolVar(&all, "all", false, "Sync all detected settings")

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

			fmt.Fprintln(cliStdout, "amux settings status")
			fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
			fmt.Fprintln(cliStdout)

			// Show sync status
			syncCfg := cfg.SettingsSync
			if syncCfg.Enabled {
				fmt.Fprintln(cliStdout, "Settings sync: enabled")
			} else {
				fmt.Fprintln(cliStdout, "Settings sync: disabled")
			}
			fmt.Fprintln(cliStdout)

			// Show configured files if using explicit file list
			if syncCfg.Enabled && len(syncCfg.Files) > 0 {
				fmt.Fprintln(cliStdout, "Configured to sync:")
				for _, f := range syncCfg.Files {
					fmt.Fprintf(cliStdout, "  %s\n", f)
				}
				fmt.Fprintln(cliStdout)
			}

			// Show all detected local settings files
			fmt.Fprintln(cliStdout, "Local files detected:")
			detected := sandbox.DetectLocalSettings()

			for _, s := range detected {
				if s.Exists {
					syncing := isSyncing(s.HomePath, syncCfg)
					status := formatFileSize(s.Size)
					if syncing {
						status += " (syncing)"
					}
					fmt.Fprintf(cliStdout, "  ~/%s (%s)\n", s.HomePath, status)
				}
			}

			// Show files that don't exist
			var notFound []string
			for _, s := range detected {
				if !s.Exists {
					notFound = append(notFound, s.HomePath)
				}
			}
			if len(notFound) > 0 {
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Not found:")
				for _, f := range notFound {
					fmt.Fprintf(cliStdout, "  ~/%s\n", f)
				}
			}

			fmt.Fprintln(cliStdout)
			fmt.Fprintln(cliStdout, strings.Repeat("─", 50))

			if !syncCfg.Enabled {
				fmt.Fprintln(cliStdout, "Run `amux settings sync --enable --all` to sync settings")
			}

			return nil
		},
	}

	return cmd
}

// isSyncing checks if a file path is configured for syncing
func isSyncing(homePath string, cfg sandbox.SettingsSyncConfig) bool {
	if !cfg.Enabled {
		return false
	}

	// Check explicit Files list first
	if len(cfg.Files) > 0 {
		for _, f := range cfg.Files {
			// Normalize path for comparison
			normalized := strings.TrimPrefix(f, "~/")
			if normalized == homePath {
				return true
			}
		}
		return false
	}

	// Fall back to legacy flags
	if strings.Contains(homePath, ".claude") && cfg.Claude {
		return true
	}
	if strings.Contains(homePath, ".codex") && cfg.Codex {
		return true
	}
	if strings.Contains(homePath, ".gitconfig") && cfg.Git {
		return true
	}
	return false
}

// formatFileSize formats a file size in human-readable form
func formatFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%.1f KB", float64(size)/1024)
}

func showSettingsSyncStatus(cfg sandbox.SettingsSyncConfig) error {
	fmt.Fprintln(cliStdout, "amux settings sync status")
	fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
	fmt.Fprintln(cliStdout)

	if cfg.Enabled {
		fmt.Fprintln(cliStdout, "Settings sync: enabled")
		fmt.Fprintln(cliStdout)

		// Show explicit file list if available
		if len(cfg.Files) > 0 {
			fmt.Fprintln(cliStdout, "Configured files:")
			for _, f := range cfg.Files {
				note := ""
				if strings.Contains(f, ".gitconfig") {
					note = " (safe keys only)"
				}
				fmt.Fprintf(cliStdout, "  %s%s\n", f, note)
			}
		} else {
			// Fall back to legacy display
			fmt.Fprintln(cliStdout, "Syncing:")
			if cfg.Claude {
				fmt.Fprintln(cliStdout, "  ✓ ~/.claude/settings.json")
			}
			if cfg.Codex {
				fmt.Fprintln(cliStdout, "  ✓ ~/.codex/config.toml")
			}
			if cfg.Git {
				fmt.Fprintln(cliStdout, "  ✓ ~/.gitconfig (safe keys)")
			}
			if !cfg.Claude && !cfg.Codex && !cfg.Git {
				fmt.Fprintln(cliStdout, "  (no settings selected)")
			}
		}
	} else {
		fmt.Fprintln(cliStdout, "Settings sync: disabled")
		fmt.Fprintln(cliStdout)
		fmt.Fprintln(cliStdout, "Enable with: amux settings sync --enable --all")
	}

	fmt.Fprintln(cliStdout)
	fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
	return nil
}

func buildSettingsShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show what would sync to a sandbox (dry-run preview)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}

			fmt.Fprintln(cliStdout, "amux settings show")
			fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
			fmt.Fprintln(cliStdout)

			syncCfg := cfg.SettingsSync
			if !syncCfg.Enabled {
				fmt.Fprintln(cliStdout, "Settings sync is disabled.")
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Enable with: amux settings sync --enable --all")
				return nil
			}

			// Get the files that would sync
			detected := sandbox.DetectLocalSettings()
			var willSync []sandbox.DetectedSetting

			for _, s := range detected {
				if s.Exists && isSyncing(s.HomePath, syncCfg) {
					willSync = append(willSync, s)
				}
			}

			if len(willSync) == 0 {
				fmt.Fprintln(cliStdout, "No settings files would sync.")
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Either no files are configured or they don't exist locally.")
				return nil
			}

			fmt.Fprintln(cliStdout, "Would sync to sandbox:")
			fmt.Fprintln(cliStdout)
			for _, s := range willSync {
				note := ""
				if strings.Contains(s.HomePath, ".gitconfig") {
					note = " (filtered: user.*, core.*, alias.*)"
				}
				fmt.Fprintf(cliStdout, "  ~/%s → ~/%s%s\n", s.HomePath, s.HomePath, note)
			}
			fmt.Fprintln(cliStdout)
			fmt.Fprintln(cliStdout, strings.Repeat("─", 50))

			return nil
		},
	}

	return cmd
}
