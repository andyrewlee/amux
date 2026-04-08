package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

const doctorLogsUnavailableMarker = "__AMUX_LOGS_UNAVAILABLE__"

const doctorLogsDmesgSnapshotCmd = `snapshot_file=$(mktemp) || exit $?
cleanup() {
  rm -f "$snapshot_file"
}
trap cleanup EXIT

command -v dmesg >/dev/null 2>&1 || {
  printf '%s'
  exit 0
}

dmesg >"$snapshot_file" 2>/dev/null
status=$?
if [ "$status" -ne 0 ]; then
  if [ "$status" -eq 1 ]; then
    printf '%s'
    exit 0
  fi
  exit "$status"
fi
tail -n %d "$snapshot_file"`

func doctorLogsNormalizeUnavailable(resp *sandbox.ExecResult) (*sandbox.ExecResult, bool) {
	if resp == nil || resp.ExitCode != 0 {
		return resp, false
	}
	if strings.TrimSpace(resp.Stdout) != doctorLogsUnavailableMarker {
		return resp, false
	}
	normalized := *resp
	normalized.Stdout = ""
	return &normalized, true
}

func doctorLogsIsJournalUnavailableFailure(resp *sandbox.ExecResult) bool {
	if resp == nil || resp.ExitCode == 0 {
		return false
	}
	if resp.ExitCode == 127 {
		return true
	}

	trimmed := strings.TrimSpace(resp.Stdout)
	if trimmed == "" {
		return false
	}
	for _, line := range strings.Split(trimmed, "\n") {
		switch strings.TrimSpace(line) {
		case "-- No entries --", "No journal files were found.":
			continue
		default:
			return false
		}
	}
	return true
}

func doctorLogsFetchSnapshot(ctx context.Context, sb sandbox.RemoteSandbox, lines int) (*sandbox.ExecResult, bool, error) {
	resp, err := sb.Exec(ctx, doctorLogsJournalctlCmd(lines, false), nil)
	if err != nil {
		return nil, false, fmt.Errorf("could not retrieve logs: %w", err)
	}
	if resp == nil {
		return nil, false, errors.New("could not retrieve logs")
	}
	if resp.ExitCode == 0 {
		resp, unavailable := doctorLogsNormalizeUnavailable(resp)
		return resp, unavailable, nil
	}
	if !doctorLogsIsJournalUnavailableFailure(resp) {
		return resp, false, nil
	}

	resp, err = sb.Exec(ctx, doctorLogsDmesgCmd(lines, false), nil)
	if err != nil {
		return nil, false, fmt.Errorf("could not retrieve logs: %w", err)
	}
	if resp == nil {
		return nil, false, errors.New("could not retrieve logs")
	}
	resp, unavailable := doctorLogsNormalizeUnavailable(resp)
	return resp, unavailable, nil
}

func doctorLogsPrintResponse(resp *sandbox.ExecResult) error {
	if resp == nil {
		return errors.New("could not retrieve logs")
	}
	if resp.ExitCode != 0 {
		if resp.Stdout != "" {
			fmt.Fprint(cliStdout, resp.Stdout)
		}
		return exitError{code: resp.ExitCode}
	}
	if resp.Stdout != "" {
		fmt.Fprint(cliStdout, resp.Stdout)
	} else {
		fmt.Fprintln(cliStdout, "No logs available")
	}
	return nil
}

// buildLogsCommand creates the logs command for viewing sandbox output.
func buildLogsCommand() *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View sandbox logs and output",
		Long:  "View logs and output from the current workspace's sandbox.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if lines <= 0 {
				return errors.New("--lines must be > 0")
			}

			cwd, err := currentCLIWorkingDir()
			if err != nil {
				return err
			}

			cfg, err := loadCLIConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := resolveCLIProvider(cfg, cwd, "")
			if err != nil {
				return err
			}

			meta, err := loadCLISandboxMeta(cwd, providerInstance.Name())
			if err != nil {
				return err
			}
			if meta == nil {
				return errors.New("no sandbox exists for this project - run `amux sandbox run <agent>` first")
			}

			sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
			if err != nil {
				if !sandbox.IsNotFoundError(err) {
					return err
				}
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

			if follow {
				ctx, cancel := doctorLogsFollowContext()
				defer cancel()
				return doctorLogsFollow(ctx, sb, lines, doctorLogsFollowInterval)
			}

			resp, _, err := doctorLogsFetchSnapshot(context.Background(), sb, lines)
			if err != nil {
				return err
			}
			return doctorLogsPrintResponse(resp)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show")

	return cmd
}
