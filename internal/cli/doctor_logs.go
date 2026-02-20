package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// buildLogsCommand creates the logs command for viewing sandbox output.
func buildLogsCommand() *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View sandbox logs and output",
		Long:  "View logs and output from the current workspace's sandbox.",
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
				return errors.New("no sandbox exists for this project - run `amux sandbox run <agent>` first")
			}

			sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
			if err != nil {
				return errors.New("sandbox not found - run `amux sandbox run <agent>` to create one")
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

			// Get logs from sandbox
			logCmd := fmt.Sprintf("journalctl --no-pager -n %d", lines)
			if follow {
				logCmd = "journalctl -f"
			}

			resp, err := sb.Exec(context.Background(), logCmd, nil)
			if err != nil {
				// Fallback to dmesg
				resp, err = sb.Exec(context.Background(), fmt.Sprintf("dmesg | tail -n %d", lines), nil)
				if err != nil {
					return fmt.Errorf("could not retrieve logs: %w", err)
				}
			}

			if resp.Stdout != "" {
				fmt.Fprint(cliStdout, resp.Stdout)
			} else {
				fmt.Fprintln(cliStdout, "No logs available")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show")

	return cmd
}
