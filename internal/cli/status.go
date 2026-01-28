package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// StatusOutput represents the JSON output for status command
type StatusOutput struct {
	SandboxID         string  `json:"sandbox_id"`
	State             string  `json:"state"`
	Agent             string  `json:"agent"`
	CPUCores          float32 `json:"cpu_cores,omitempty"`
	MemoryGB          float32 `json:"memory_gb,omitempty"`
	Provider          string  `json:"provider"`
	PersistenceVolume string  `json:"persistence_volume,omitempty"`
	Exists            bool    `json:"exists"`
}

func buildStatusCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current project sandbox status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, providerName, err := sandbox.ResolveProvider(cfg, cwd, "")
			if err != nil {
				return err
			}

			meta, err := sandbox.LoadSandboxMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				if jsonOutput {
					output := StatusOutput{
						Exists:            false,
						Provider:          providerName,
						PersistenceVolume: sandbox.ResolvePersistenceVolumeName(cfg),
					}
					data, _ := json.MarshalIndent(output, "", "  ")
					fmt.Println(string(data))
					return nil
				}
				fmt.Println("No sandbox for this project")
				fmt.Println("Run `amux sandbox run <agent>` to create one")
				return nil
			}

			sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
			if err != nil {
				if jsonOutput {
					output := StatusOutput{
						SandboxID:         meta.SandboxID,
						Agent:             string(meta.Agent),
						Exists:            false,
						Provider:          providerName,
						PersistenceVolume: sandbox.ResolvePersistenceVolumeName(cfg),
					}
					data, _ := json.MarshalIndent(output, "", "  ")
					fmt.Println(string(data))
					return nil
				}
				fmt.Println("Sandbox not found (may have been deleted)")
				fmt.Printf("  Sandbox ID:   %s\n", meta.SandboxID)
				fmt.Printf("  Last agent:   %s\n", meta.Agent)
				fmt.Println("\nRun `amux sandbox run <agent>` to create a new one")
				return nil
			}

			if jsonOutput {
				output := StatusOutput{
					SandboxID:         sb.ID(),
					State:             string(sb.State()),
					Agent:             string(meta.Agent),
					Provider:          providerName,
					Exists:            true,
					PersistenceVolume: sandbox.ResolvePersistenceVolumeName(cfg),
				}
				if resources, ok := sb.(sandbox.SandboxResources); ok {
					output.CPUCores = resources.CPUCores()
					output.MemoryGB = resources.MemoryGB()
				}
				data, _ := json.MarshalIndent(output, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Println("amux sandbox status")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Println()
			fmt.Printf("  Sandbox ID:   %s\n", sb.ID())
			fmt.Printf("  State:        %s\n", stateWithColor(string(sb.State())))
			fmt.Printf("  Agent:        %s\n", meta.Agent)
			fmt.Printf("  Persistence: %s\n", sandbox.ResolvePersistenceVolumeName(cfg))
			if resources, ok := sb.(sandbox.SandboxResources); ok {
				fmt.Printf("  Resources:    %.1f CPU, %.1f GiB RAM\n", resources.CPUCores(), resources.MemoryGB())
			}

			if sb.State() == sandbox.StateStarted {
				fmt.Println()
				fmt.Println("  Ready for:")
				fmt.Printf("    amux ssh              # raw shell access\n")
				fmt.Printf("    amux exec <cmd>       # run a command\n")
				fmt.Printf("    amux sandbox run %s  # interactive session\n", meta.Agent)
			} else if sb.State() == sandbox.StateStopped {
				fmt.Println()
				fmt.Println("  Sandbox is stopped. Run `amux sandbox run <agent>` to start it.")
			}

			fmt.Println()
			fmt.Println(strings.Repeat("─", 50))
			return nil
		},
	}
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
	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "Open a raw SSH shell to the current project sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := sandbox.ResolveProvider(cfg, cwd, "")
			if err != nil {
				return err
			}
			meta, err := sandbox.LoadSandboxMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("no sandbox for this project - run `amux sandbox run <agent>` first")
			}

			sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
			if err != nil {
				return fmt.Errorf("sandbox not found - run `amux sandbox run <agent>` to create one")
			}

			if sb.State() != sandbox.StateStarted {
				fmt.Fprintln(os.Stderr, "Starting sandbox...")
				if err := sb.Start(context.Background()); err != nil {
					return fmt.Errorf("failed to start sandbox: %w", err)
				}
				if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: sandbox may not be fully ready: %v\n", err)
				}
			}

			worktreeID := sandbox.ComputeWorktreeID(cwd)
			workspacePath := sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{Cwd: cwd, WorktreeID: worktreeID})

			id := sb.ID()
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Printf("Connecting to sandbox %s...\n", id)
			exitCode, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
				Agent:         sandbox.AgentShell,
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
	return cmd
}

func buildExecCommand() *cobra.Command {
	var workdir string

	cmd := &cobra.Command{
		Use:   "exec <command> [args...]",
		Short: "Execute a command in the current project sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := sandbox.ResolveProvider(cfg, cwd, "")
			if err != nil {
				return err
			}
			meta, err := sandbox.LoadSandboxMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("no sandbox for this project - run `amux sandbox run <agent>` first")
			}

			sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
			if err != nil {
				return fmt.Errorf("sandbox not found - run `amux sandbox run <agent>` to create one")
			}

			if sb.State() != sandbox.StateStarted {
				fmt.Fprintln(os.Stderr, "Starting sandbox...")
				if err := sb.Start(context.Background()); err != nil {
					return fmt.Errorf("failed to start sandbox: %w", err)
				}
				if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: sandbox may not be fully ready: %v\n", err)
				}
			}

			execPath := workdir
			if execPath == "" {
				worktreeID := sandbox.ComputeWorktreeID(cwd)
				execPath = sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{Cwd: cwd, WorktreeID: worktreeID})
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

	return cmd
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
