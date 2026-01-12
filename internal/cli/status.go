package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

// StatusOutput represents the JSON output for status command
type StatusOutput struct {
	ComputerID string  `json:"computer_id"`
	State      string  `json:"state"`
	Agent      string  `json:"agent"`
	CPUCores   float32 `json:"cpu_cores,omitempty"`
	MemoryGB   float32 `json:"memory_gb,omitempty"`
	Provider   string  `json:"provider"`
	Exists     bool    `json:"exists"`
}

func buildStatusCommand() *cobra.Command {
	var provider string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current project computer status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, providerName, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}

			meta, err := computer.LoadComputerMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				if jsonOutput {
					output := StatusOutput{Exists: false, Provider: providerName}
					data, _ := json.MarshalIndent(output, "", "  ")
					fmt.Println(string(data))
					return nil
				}
				fmt.Println("No computer for this project")
				fmt.Println("Run `amux computer run <agent>` to create one")
				return nil
			}

			sb, err := providerInstance.GetComputer(context.Background(), meta.ComputerID)
			if err != nil {
				if jsonOutput {
					output := StatusOutput{
						ComputerID: meta.ComputerID,
						Agent:      string(meta.Agent),
						Exists:     false,
						Provider:   providerName,
					}
					data, _ := json.MarshalIndent(output, "", "  ")
					fmt.Println(string(data))
					return nil
				}
				fmt.Println("Computer not found (may have been deleted)")
				fmt.Printf("  Computer ID:   %s\n", meta.ComputerID)
				fmt.Printf("  Last agent:   %s\n", meta.Agent)
				fmt.Println("\nRun `amux computer run <agent>` to create a new one")
				return nil
			}

			if jsonOutput {
				output := StatusOutput{
					ComputerID: sb.ID(),
					State:      string(sb.State()),
					Agent:      string(meta.Agent),
					Provider:   providerName,
					Exists:     true,
				}
				if resources, ok := sb.(computer.ComputerResources); ok {
					output.CPUCores = resources.CPUCores()
					output.MemoryGB = resources.MemoryGB()
				}
				data, _ := json.MarshalIndent(output, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Println("amux computer status")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Println()
			fmt.Printf("  Computer ID:   %s\n", sb.ID())
			fmt.Printf("  State:        %s\n", stateWithColor(string(sb.State())))
			fmt.Printf("  Agent:        %s\n", meta.Agent)
			if resources, ok := sb.(computer.ComputerResources); ok {
				fmt.Printf("  Resources:    %.1f CPU, %.1f GiB RAM\n", resources.CPUCores(), resources.MemoryGB())
			}

			if sb.State() == computer.StateStarted {
				fmt.Println()
				fmt.Println("  Ready for:")
				fmt.Printf("    amux ssh              # raw shell access\n")
				fmt.Printf("    amux exec <cmd>       # run a command\n")
				fmt.Printf("    amux computer run %s  # interactive session\n", meta.Agent)
			} else if sb.State() == computer.StateStopped {
				fmt.Println()
				fmt.Println("  Computer is stopped. Run `amux computer run <agent>` to start it.")
			}

			fmt.Println()
			fmt.Println(strings.Repeat("─", 50))
			return nil
		},
	}
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	return cmd
}

func stateWithColor(state string) string {
	switch state {
	case "started":
		return state + " (running)"
	case "stopped":
		return state
	case "pending":
		return state + " (starting...)"
	default:
		return state
	}
}

func buildSSHCommand() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "Open a raw SSH shell to the current project computer",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}
			meta, err := computer.LoadComputerMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("no computer for this project - run `amux computer run <agent>` first")
			}

			sb, err := providerInstance.GetComputer(context.Background(), meta.ComputerID)
			if err != nil {
				return fmt.Errorf("computer not found - run `amux computer run <agent>` to create one")
			}

			if sb.State() != computer.StateStarted {
				fmt.Fprintln(os.Stderr, "Starting computer...")
				if err := sb.Start(context.Background()); err != nil {
					return fmt.Errorf("failed to start computer: %w", err)
				}
				if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: computer may not be fully ready: %v\n", err)
				}
			}

			worktreeID := computer.ComputeWorktreeID(cwd)
			workspacePath := computer.GetWorktreeRepoPath(sb, computer.SyncOptions{Cwd: cwd, WorktreeID: worktreeID})

			id := sb.ID()
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Printf("Connecting to computer %s...\n", id)
			exitCode, err := computer.RunAgentInteractive(sb, computer.AgentConfig{
				Agent:         computer.AgentShell,
				WorkspacePath: workspacePath,
				Args:          []string{},
				Env:           map[string]string{},
			})
			if err != nil {
				return err
			}
			if exitCode != 0 {
				return exitError{code: exitCode}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	return cmd
}

func buildExecCommand() *cobra.Command {
	var workdir string
	var provider string

	cmd := &cobra.Command{
		Use:   "exec <command> [args...]",
		Short: "Execute a command in the current project computer",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}
			meta, err := computer.LoadComputerMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("no computer for this project - run `amux computer run <agent>` first")
			}

			sb, err := providerInstance.GetComputer(context.Background(), meta.ComputerID)
			if err != nil {
				return fmt.Errorf("computer not found - run `amux computer run <agent>` to create one")
			}

			if sb.State() != computer.StateStarted {
				fmt.Fprintln(os.Stderr, "Starting computer...")
				if err := sb.Start(context.Background()); err != nil {
					return fmt.Errorf("failed to start computer: %w", err)
				}
				if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: computer may not be fully ready: %v\n", err)
				}
			}

			execPath := workdir
			if execPath == "" {
				worktreeID := computer.ComputeWorktreeID(cwd)
				execPath = computer.GetWorktreeRepoPath(sb, computer.SyncOptions{Cwd: cwd, WorktreeID: worktreeID})
			}

			// Build command string
			cmdStr := strings.Join(args, " ")
			fullCmd := fmt.Sprintf("cd %s && %s", quoteShell(execPath), cmdStr)

			resp, err := sb.Exec(context.Background(), fullCmd, nil)
			if err != nil {
				return err
			}

			// Print output
			if resp.Stdout != "" {
				fmt.Print(resp.Stdout)
			}

			if resp.ExitCode != 0 {
				return exitError{code: resp.ExitCode}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&workdir, "workdir", "w", "", "Working directory (default: worktree repo path)")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")

	return cmd
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
