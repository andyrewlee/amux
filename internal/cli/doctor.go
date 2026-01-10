package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildDoctorCommand() *cobra.Command {
	var checkAll bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check Daytona connectivity, snapshot config, and credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}

			fmt.Println("amux doctor")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Println()

			hasIssue := false

			// Check 1: Daytona API key
			apiKey := sandbox.ResolveAPIKey(cfg)
			if apiKey != "" {
				fmt.Println("✓ Daytona API key is configured")
			} else {
				fmt.Println("✗ Daytona API key missing")
				fmt.Println("  Run: amux auth login")
				hasIssue = true
			}

			// Check 2: Daytona client & API connectivity
			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				fmt.Printf("✗ Daytona client error: %v\n", err)
				hasIssue = true
			} else {
				// Test actual API connectivity by listing sandboxes
				if _, err := client.List(); err != nil {
					fmt.Printf("✗ Daytona API unreachable: %v\n", err)
					hasIssue = true
				} else {
					fmt.Println("✓ Daytona API connection working")
				}
			}

			// Check 3: Credentials volume
			if client != nil {
				if _, err := client.Volume.Get(sandbox.CredentialsVolumeName, true); err != nil {
					fmt.Println("✗ Credentials volume missing")
					fmt.Println("  Run: amux setup")
					hasIssue = true
				} else {
					fmt.Println("✓ Credentials volume ready")
				}
			}

			// Check 4: Snapshot
			snapshotName := sandbox.ResolveSnapshotID(cfg)
			if snapshotName != "" {
				if client != nil {
					if snap, err := client.Snapshot.Get(snapshotName); err == nil {
						fmt.Printf("✓ Snapshot \"%s\" is %s\n", snap.Name, snap.State)
					} else {
						fmt.Printf("✗ Snapshot \"%s\" not found\n", snapshotName)
						fmt.Println("  Run: amux setup")
						hasIssue = true
					}
				}
			} else {
				fmt.Println("• No default snapshot configured")
				fmt.Println("  Run: amux setup")
				hasIssue = true
			}

			// Check 5: Optional credential health (--all flag)
			if checkAll {
				fmt.Println()
				fmt.Println("Credential health:")

				// Anthropic API key
				anthropicKey := cfg.AnthropicAPIKey
				if anthropicKey == "" {
					anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
				}
				if anthropicKey != "" {
					fmt.Println("  ✓ Anthropic API key configured")
				} else {
					fmt.Println("  • Anthropic API key not set (needed for Claude)")
				}

				// OpenAI API key
				openaiKey := cfg.OpenAIAPIKey
				if openaiKey == "" {
					openaiKey = os.Getenv("OPENAI_API_KEY")
				}
				if openaiKey != "" {
					fmt.Println("  ✓ OpenAI API key configured")
				} else {
					fmt.Println("  • OpenAI API key not set (needed for Codex)")
				}

				// Gemini API key
				geminiKey := os.Getenv("GEMINI_API_KEY")
				if geminiKey == "" {
					geminiKey = os.Getenv("GOOGLE_API_KEY")
				}
				if geminiKey != "" {
					fmt.Println("  ✓ Gemini API key configured")
				} else {
					fmt.Println("  • Gemini API key not set (needed for Gemini CLI)")
				}
			}

			// Check 6: Current workspace sandbox (if in a workspace)
			cwd, err := os.Getwd()
			if err == nil {
				meta, _ := sandbox.LoadWorkspaceMeta(cwd)
				if meta != nil {
					fmt.Println()
					fmt.Println("Current workspace:")
					if client != nil {
						if sb, err := client.Get(meta.SandboxID); err == nil {
							fmt.Printf("  ✓ Sandbox %s (%s)\n", sb.ID[:8], sb.State)
						} else {
							fmt.Printf("  • Sandbox %s not found (may have been deleted)\n", meta.SandboxID[:8])
						}
					}
				}
			}

			fmt.Println()
			fmt.Println(strings.Repeat("─", 50))

			if !hasIssue {
				fmt.Println("All checks passed. Ready to run `amux claude`.")
			} else {
				fmt.Println("Some issues found. Fix them and run `amux doctor` again.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkAll, "all", false, "Check all credentials (Anthropic, OpenAI, Gemini)")

	return cmd
}
