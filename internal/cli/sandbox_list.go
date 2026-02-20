package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildSandboxPreviewCommand() *cobra.Command {
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "preview <port>",
		Short: "Open a browser preview for a sandbox port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil || port <= 0 || port > 65535 {
				return fmt.Errorf("port must be a number between 1 and 65535")
			}
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
			if !providerInstance.SupportsFeature(sandbox.FeaturePreviewURLs) {
				return fmt.Errorf("preview URLs are not supported by the selected provider")
			}
			fmt.Fprintf(cliStdout, "Preparing preview for port %d...\n", port)
			sb, _, err := resolveCurrentSandbox(providerInstance, cwd)
			if err != nil {
				return err
			}
			url, err := sb.GetPreviewURL(context.Background(), port)
			if err != nil {
				return err
			}
			if url == "" {
				return fmt.Errorf("unable to construct a preview URL")
			}
			fmt.Fprintf(cliStdout, "Preview URL: %s\n", url)
			if !noOpen {
				if !tryOpenURL(url) {
					fmt.Fprintln(cliStdout, "Open the URL in your browser.")
				}
			}
			fmt.Fprintf(cliStdout, "Tip: Ensure your app listens on 0.0.0.0:%d inside the sandbox.\n", port)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the URL automatically")
	return cmd
}

func buildSandboxLogsCommand() *cobra.Command {
	var follow bool
	var lines int
	var list bool
	var file string
	var spin bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View recorded agent logs for this workspace",
		Long:  "View recorded agent session output stored on the persistent volume.",
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
			if list && file != "" {
				return fmt.Errorf("cannot use --list with --file")
			}

			worktreeID := sandbox.ComputeWorktreeID(cwd)
			logDir := fmt.Sprintf("/amux/logs/%s", worktreeID)
			resolveSandbox := func() (sandbox.RemoteSandbox, func(), error) {
				meta, err := sandbox.LoadSandboxMeta(cwd, providerInstance.Name())
				if err != nil {
					return nil, nil, err
				}
				if meta != nil {
					sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
					if err == nil {
						if err := sb.Start(context.Background()); err == nil {
							if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil && Verbose {
								fmt.Fprintf(os.Stderr, "Warning: sandbox may not be fully ready: %v\n", err)
							}
							return sb, nil, nil
						}
					}
				}
				if !spin {
					return nil, nil, fmt.Errorf("no running sandbox found; re-run with --spin to start a log reader sandbox")
				}
				if !providerInstance.SupportsFeature(sandbox.FeatureVolumes) {
					return nil, nil, fmt.Errorf("persistent logs require volume support from the provider")
				}
				volumeMgr := providerInstance.Volumes()
				if volumeMgr == nil {
					return nil, nil, fmt.Errorf("volume manager is not available")
				}
				volumeName := sandbox.ResolvePersistenceVolumeName(cfg)
				volume, err := volumeMgr.GetOrCreate(context.Background(), volumeName)
				if err != nil {
					return nil, nil, err
				}
				if _, err := volumeMgr.WaitReady(context.Background(), volumeName, 0); err != nil {
					return nil, nil, err
				}
				labels := map[string]string{
					"amux.provider":   providerInstance.Name(),
					"amux.worktreeId": worktreeID,
					"amux.purpose":    "logs",
				}
				sb, err := providerInstance.CreateSandbox(context.Background(), sandbox.SandboxCreateConfig{
					Agent:             sandbox.AgentShell,
					Labels:            labels,
					Volumes:           []sandbox.VolumeMount{{VolumeID: volume.ID, MountPath: "/amux"}},
					AutoStopMinutes:   15,
					AutoDeleteMinutes: 20,
					Ephemeral:         true,
				})
				if err != nil {
					return nil, nil, err
				}
				if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
					return nil, nil, err
				}
				cleanup := func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_ = sb.Stop(ctx)
					_ = providerInstance.DeleteSandbox(ctx, sb.ID())
				}
				fmt.Fprintln(cliStdout, "Started a short-lived log reader sandbox.")
				return sb, cleanup, nil
			}

			sb, cleanup, err := resolveSandbox()
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}

			if list {
				resp, err := sb.Exec(context.Background(), fmt.Sprintf("ls -t %s/*.log 2>/dev/null", sandbox.ShellQuote(logDir)), nil)
				if err != nil {
					return fmt.Errorf("could not list logs: %w", err)
				}
				if strings.TrimSpace(resp.Stdout) == "" {
					fmt.Fprintln(cliStdout, "No logs found.")
					return nil
				}
				fmt.Fprint(cliStdout, resp.Stdout)
				return nil
			}

			logPath := strings.TrimSpace(file)
			if logPath == "" {
				listCmd := fmt.Sprintf("ls -t %s/*.log 2>/dev/null | head -n 1", sandbox.ShellQuote(logDir))
				resp, err := sb.Exec(context.Background(), listCmd, nil)
				if err != nil {
					return fmt.Errorf("could not list logs: %w", err)
				}
				logPath = strings.TrimSpace(resp.Stdout)
				if logPath == "" {
					return fmt.Errorf("no recorded logs found for this workspace; run `amux sandbox run <agent> --record`")
				}
			} else if !strings.Contains(logPath, "/") {
				logPath = fmt.Sprintf("%s/%s", logDir, logPath)
			}

			if follow {
				fmt.Fprintf(cliStdout, "Tailing %s (Ctrl+C to stop)\n", logPath)
				_, err := sb.ExecInteractive(context.Background(),
					fmt.Sprintf("tail -n %d -f %s", lines, sandbox.ShellQuote(logPath)),
					os.Stdin, os.Stdout, os.Stderr, nil)
				return err
			}

			resp, err := sb.Exec(context.Background(), fmt.Sprintf("tail -n %d %s", lines, sandbox.ShellQuote(logPath)), nil)
			if err != nil {
				return err
			}
			if resp.Stdout != "" {
				fmt.Fprint(cliStdout, resp.Stdout)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 200, "Number of lines to show")
	cmd.Flags().BoolVar(&list, "list", false, "List recorded logs for this workspace")
	cmd.Flags().StringVar(&file, "file", "", "Show a specific log file (basename or full path)")
	cmd.Flags().BoolVar(&spin, "spin", true, "Start a short-lived sandbox if none is running")
	return cmd
}

func buildSandboxDesktopCommand() *cobra.Command {
	var port string
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "desktop",
		Short: "Open a remote desktop (VNC) for the sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := strconv.Atoi(port)
			if err != nil || p <= 0 || p > 65535 {
				return fmt.Errorf("port must be a number between 1 and 65535")
			}
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
			if !providerInstance.SupportsFeature(sandbox.FeatureDesktop) {
				return fmt.Errorf("desktop is not supported by the selected provider")
			}
			fmt.Fprintln(cliStdout, "Checking desktop status...")
			sb, _, err := resolveCurrentSandbox(providerInstance, cwd)
			if err != nil {
				return err
			}
			desktop, ok := sb.(sandbox.DesktopAccess)
			if !ok {
				return fmt.Errorf("desktop is not available for this provider")
			}
			status, err := desktop.DesktopStatus(context.Background())
			if err != nil {
				return fmt.Errorf("desktop is not available in this sandbox image. Tip: use a desktop-enabled base image and rebuild your snapshot")
			}
			if status == nil || status.Status != "active" {
				fmt.Fprintln(cliStdout, "Starting desktop...")
				if err := desktop.StartDesktop(context.Background()); err != nil {
					return fmt.Errorf("failed to start desktop services. Tip: your snapshot may be missing VNC dependencies (xvfb/novnc)")
				}
				time.Sleep(5 * time.Second)
				status, err = desktop.DesktopStatus(context.Background())
				if err != nil {
					return err
				}
			}
			if status == nil || status.Status != "active" {
				return fmt.Errorf("desktop failed to start (status: %s)", func() string {
					if status == nil {
						return "unknown"
					}
					return status.Status
				}())
			}
			url, err := sb.GetPreviewURL(context.Background(), p)
			if err != nil {
				return err
			}
			url = buildVncURL(url)
			if url == "" {
				return fmt.Errorf("unable to construct the desktop URL")
			}
			fmt.Fprintf(cliStdout, "Desktop URL: %s\n", url)
			if !noOpen {
				if !tryOpenURL(url) {
					fmt.Fprintln(cliStdout, "Open the URL in your browser.")
				}
			}
			fmt.Fprintln(cliStdout, "Tip: If the page is blank, wait a few seconds and refresh.")
			return nil
		},
	}
	cmd.Flags().StringVar(&port, "port", "6080", "VNC port (default: 6080)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the URL automatically")
	return cmd
}

// SandboxListItem represents a single sandbox in JSON output
type SandboxListItem struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Agent   string `json:"agent"`
	Project string `json:"project,omitempty"`
}

func buildSandboxLsCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List all amux sandboxes",
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
			sandboxes, err := sandbox.ListAmuxSandboxes(providerInstance)
			if err != nil {
				return err
			}

			if jsonOutput {
				items := make([]SandboxListItem, 0, len(sandboxes))
				for _, sb := range sandboxes {
					item := SandboxListItem{
						ID:    sb.ID(),
						State: string(sb.State()),
						Agent: "unknown",
					}
					labels := sb.Labels()
					if labels != nil {
						if val, ok := labels["amux.agent"]; ok {
							item.Agent = val
						}
						if val, ok := labels["amux.project"]; ok {
							item.Project = val
						}
					}
					items = append(items, item)
				}
				data, _ := json.MarshalIndent(items, "", "  ")
				fmt.Fprintln(cliStdout, string(data))
				return nil
			}

			if len(sandboxes) == 0 {
				fmt.Fprintln(cliStdout, "No sandboxes found")
				return nil
			}
			fmt.Fprintf(cliStdout, "%-12s %-10s %-10s %s\n", "ID", "STATE", "AGENT", "PROJECT")
			fmt.Fprintln(cliStdout, strings.Repeat("─", 60))
			for _, sb := range sandboxes {
				agent := "unknown"
				project := "unknown"
				labels := sb.Labels()
				if labels != nil {
					if val, ok := labels["amux.agent"]; ok {
						agent = val
					}
					if val, ok := labels["amux.project"]; ok {
						project = val
					}
				}
				id := sb.ID()
				if len(id) > 12 {
					id = id[:12]
				}
				fmt.Fprintf(cliStdout, "%-12s %-10s %-10s %s\n", id, sb.State(), agent, project)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	return cmd
}
