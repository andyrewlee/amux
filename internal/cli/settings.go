package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

func buildSettingsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage local settings sync to computers",
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
		Short: "Configure which local settings to sync to computers",
		Long: `Configure which local settings files to sync to cloud computers.

Settings sync is opt-in and requires explicit consent. When enabled, amux will
copy your local configuration files (like ~/.claude/settings.json) to the
computer so your preferences are available in cloud sessions.

IMPORTANT: Settings sync only copies non-sensitive configuration. API keys,
tokens, and credentials are automatically filtered out.

Examples:
  amux settings sync --enable --claude    # Enable Claude settings sync
  amux settings sync --enable --all       # Enable all detected settings
  amux settings sync --disable            # Disable all settings sync
  amux settings sync                      # Show current sync status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := computer.LoadConfig()
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
				if err := computer.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Println("Settings sync disabled")
				return nil
			}

			// Handle enable flag
			if enable {
				// Detect existing settings files
				detected := computer.DetectExistingSettings()

				fmt.Println("amux settings sync")
				fmt.Println(strings.Repeat("─", 50))
				fmt.Println()

				// Show detected files
				if len(detected) > 0 {
					fmt.Println("Detected settings files:")
					for _, s := range detected {
						fmt.Printf("  ~/%s (%s)\n", s.HomePath, s.Description)
					}
					fmt.Println()
				} else {
					fmt.Println("No settings files detected locally.")
					fmt.Println()
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
						fmt.Println("Specify which settings to sync:")
						fmt.Println("  --all      Sync all detected files")
						fmt.Println("  --claude   Sync Claude settings")
						fmt.Println("  --codex    Sync Codex settings")
						fmt.Println("  --git      Sync git config")
						fmt.Println()
						fmt.Println("Example: amux settings sync --enable --all")
						return nil
					}
				}

				if len(filesToSync) == 0 {
					fmt.Println("No matching settings files to sync.")
					return nil
				}

				// Show what will be synced
				fmt.Println("Will sync these files to computer:")
				for _, f := range filesToSync {
					note := ""
					if strings.Contains(f, ".gitconfig") {
						note = " (safe keys only)"
					}
					fmt.Printf("  %s%s\n", f, note)
				}
				fmt.Println()
				fmt.Println("Note: API keys and tokens are automatically filtered out.")
				fmt.Println()

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
				if err := computer.SaveConfig(cfg); err != nil {
					return err
				}

				fmt.Println("✓ Settings sync enabled")
				fmt.Println()
				fmt.Println("Files will sync on next `amux claude/codex/...` run.")
				fmt.Println("To disable: amux settings sync --disable")
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
			cfg, err := computer.LoadConfig()
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

			// Show configured files if using explicit file list
			if syncCfg.Enabled && len(syncCfg.Files) > 0 {
				fmt.Println("Configured to sync:")
				for _, f := range syncCfg.Files {
					fmt.Printf("  %s\n", f)
				}
				fmt.Println()
			}

			// Show all detected local settings files
			fmt.Println("Local files detected:")
			detected := computer.DetectLocalSettings()

			for _, s := range detected {
				if s.Exists {
					syncing := isSyncing(s.HomePath, syncCfg)
					status := formatFileSize(s.Size)
					if syncing {
						status += " (syncing)"
					}
					fmt.Printf("  ~/%s (%s)\n", s.HomePath, status)
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
				fmt.Println()
				fmt.Println("Not found:")
				for _, f := range notFound {
					fmt.Printf("  ~/%s\n", f)
				}
			}

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

// isSyncing checks if a file path is configured for syncing
func isSyncing(homePath string, cfg computer.SettingsSyncConfig) bool {
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

func showSettingsSyncStatus(cfg computer.SettingsSyncConfig) error {
	fmt.Println("amux settings sync status")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	if cfg.Enabled {
		fmt.Println("Settings sync: enabled")
		fmt.Println()

		// Show explicit file list if available
		if len(cfg.Files) > 0 {
			fmt.Println("Configured files:")
			for _, f := range cfg.Files {
				note := ""
				if strings.Contains(f, ".gitconfig") {
					note = " (safe keys only)"
				}
				fmt.Printf("  %s%s\n", f, note)
			}
		} else {
			// Fall back to legacy display
			fmt.Println("Syncing:")
			if cfg.Claude {
				fmt.Println("  ✓ ~/.claude/settings.json")
			}
			if cfg.Codex {
				fmt.Println("  ✓ ~/.codex/config.toml")
			}
			if cfg.Git {
				fmt.Println("  ✓ ~/.gitconfig (safe keys)")
			}
			if !cfg.Claude && !cfg.Codex && !cfg.Git {
				fmt.Println("  (no settings selected)")
			}
		}
	} else {
		fmt.Println("Settings sync: disabled")
		fmt.Println()
		fmt.Println("Enable with: amux settings sync --enable --all")
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	return nil
}

func buildSettingsShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show what would sync to a computer (dry-run preview)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}

			fmt.Println("amux settings show")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Println()

			syncCfg := cfg.SettingsSync
			if !syncCfg.Enabled {
				fmt.Println("Settings sync is disabled.")
				fmt.Println()
				fmt.Println("Enable with: amux settings sync --enable --all")
				return nil
			}

			// Get the files that would sync
			detected := computer.DetectLocalSettings()
			var willSync []computer.DetectedSetting

			for _, s := range detected {
				if s.Exists && isSyncing(s.HomePath, syncCfg) {
					willSync = append(willSync, s)
				}
			}

			if len(willSync) == 0 {
				fmt.Println("No settings files would sync.")
				fmt.Println()
				fmt.Println("Either no files are configured or they don't exist locally.")
				return nil
			}

			fmt.Println("Would sync to computer:")
			fmt.Println()
			for _, s := range willSync {
				note := ""
				if strings.Contains(s.HomePath, ".gitconfig") {
					note = " (filtered: user.*, core.*, alias.*)"
				}
				fmt.Printf("  ~/%s → ~/%s%s\n", s.HomePath, s.HomePath, note)
			}
			fmt.Println()
			fmt.Println(strings.Repeat("─", 50))

			return nil
		},
	}

	return cmd
}
