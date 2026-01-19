package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

func isReadable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tmuxNewSession builds a tmux new-session command using the same server/config
// options as other tmux helpers. This avoids importing unexported functions.
func tmuxNewSession(opts tmux.Options, extraArgs ...string) (*exec.Cmd, context.CancelFunc) {
	args := []string{}
	if opts.ServerName != "" {
		args = append(args, "-L", opts.ServerName)
	}
	if opts.ConfigPath != "" {
		args = append(args, "-f", opts.ConfigPath)
	}
	args = append(args, extraArgs...)

	timeout := 5 * time.Second
	if opts.CommandTimeout > 0 {
		timeout = opts.CommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	return cmd, cancel
}

func parseVolumeSpec(spec string) (sandbox.VolumeSpec, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return sandbox.VolumeSpec{}, fmt.Errorf("invalid volume spec %q. Use name:/path", spec)
	}
	return sandbox.VolumeSpec{Name: parts[0], MountPath: parts[1]}, nil
}

func buildVncURL(previewURL string) string {
	if previewURL == "" {
		return ""
	}
	trimmed := strings.TrimRight(previewURL, "/")
	parts := strings.SplitN(trimmed, "?", 2)
	urlBase := parts[0]
	query := ""
	if len(parts) == 2 {
		query = parts[1]
	}
	vnc := urlBase + "/vnc.html"
	if query == "" {
		return vnc
	}
	return vnc + "?" + query
}

func tryOpenURL(url string) bool {
	if url == "" {
		return false
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return false
	}
	return true
}

func getAgentArgs(argv []string, agent string) []string {
	agentIndex := -1
	for i := 0; i < len(argv)-2; i++ {
		if argv[i] == "sandbox" && argv[i+1] == "run" && argv[i+2] == agent {
			agentIndex = i + 2
			break
		}
	}
	if agentIndex == -1 {
		return nil
	}
	passthrough := -1
	for i := agentIndex + 1; i < len(argv); i++ {
		if argv[i] == "--" {
			passthrough = i
			break
		}
	}
	if passthrough == -1 {
		return nil
	}
	if passthrough+1 >= len(argv) {
		return nil
	}
	return argv[passthrough+1:]
}

func getenvFallback(keys ...string) string {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok && val != "" {
			return val
		}
	}
	return ""
}
