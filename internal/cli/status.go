package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current workspace sandbox status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			meta, err := sandbox.LoadWorkspaceMeta(cwd)
			if err != nil {
				return err
			}
			if meta == nil {
				fmt.Println("No sandbox for this workspace")
				fmt.Println("Run `amux sandbox run <agent>` to create one")
				return nil
			}

			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				return err
			}

			sb, err := client.Get(meta.SandboxID)
			if err != nil {
				fmt.Println("Sandbox not found (may have been deleted)")
				fmt.Printf("  Sandbox ID:   %s\n", meta.SandboxID)
				fmt.Printf("  Workspace ID: %s\n", meta.WorkspaceID)
				fmt.Printf("  Last agent:   %s\n", meta.Agent)
				fmt.Println("\nRun `amux sandbox run <agent>` to create a new one")
				return nil
			}

			fmt.Println("amux workspace status")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Println()
			fmt.Printf("  Sandbox ID:   %s\n", sb.ID)
			fmt.Printf("  State:        %s\n", stateWithColor(string(sb.State)))
			fmt.Printf("  Agent:        %s\n", meta.Agent)
			fmt.Printf("  Workspace ID: %s\n", meta.WorkspaceID)
			fmt.Printf("  Resources:    %.1f CPU, %.1f GiB RAM\n", sb.CPU, sb.Memory)

			if sb.State == "started" {
				fmt.Println()
				fmt.Println("  Ready for:")
				fmt.Printf("    amux ssh              # raw shell access\n")
				fmt.Printf("    amux exec <cmd>       # run a command\n")
				fmt.Printf("    amux sandbox run %s  # interactive session\n", meta.Agent)
			} else if sb.State == "stopped" {
				fmt.Println()
				fmt.Println("  Sandbox is stopped. Run `amux sandbox run <agent>` to start it.")
			}

			fmt.Println()
			fmt.Println(strings.Repeat("─", 50))
			return nil
		},
	}
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
		Short: "Open a raw SSH shell to the current workspace sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			meta, err := sandbox.LoadWorkspaceMeta(cwd)
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("no sandbox for this workspace - run `amux sandbox run <agent>` first")
			}

			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				return err
			}

			sb, err := client.Get(meta.SandboxID)
			if err != nil {
				return fmt.Errorf("sandbox not found - run `amux sandbox run <agent>` to create one")
			}

			if sb.State != "started" {
				fmt.Println("Starting sandbox...")
				if err := sb.Start(60 * time.Second); err != nil {
					return fmt.Errorf("failed to start sandbox: %w", err)
				}
			}

			workspacePath := sandbox.GetWorkspaceRepoPath(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID})

			fmt.Printf("Connecting to sandbox %s...\n", sb.ID[:8])
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
		Short: "Execute a command in the current workspace sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			meta, err := sandbox.LoadWorkspaceMeta(cwd)
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("no sandbox for this workspace - run `amux sandbox run <agent>` first")
			}

			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				return err
			}

			sb, err := client.Get(meta.SandboxID)
			if err != nil {
				return fmt.Errorf("sandbox not found - run `amux sandbox run <agent>` to create one")
			}

			if sb.State != "started" {
				fmt.Fprintln(os.Stderr, "Starting sandbox...")
				if err := sb.Start(60 * time.Second); err != nil {
					return fmt.Errorf("failed to start sandbox: %w", err)
				}
			}

			execPath := workdir
			if execPath == "" {
				execPath = sandbox.GetWorkspaceRepoPath(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID})
			}

			// Build command string
			cmdStr := strings.Join(args, " ")
			fullCmd := fmt.Sprintf("cd %s && %s", quoteShell(execPath), cmdStr)

			resp, err := sb.Process.ExecuteCommand(fullCmd)
			if err != nil {
				return err
			}

			// Print output
			if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
				fmt.Print(resp.Artifacts.Stdout)
			} else if resp.Result != "" {
				fmt.Print(resp.Result)
			}

			if resp.ExitCode != 0 {
				return exitError{code: int(resp.ExitCode)}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&workdir, "workdir", "w", "", "Working directory (default: workspace repo path)")

	return cmd
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
