package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func getAgentArgs(args []string, argsLenAtDash int) []string {
	if argsLenAtDash < 0 || argsLenAtDash >= len(args) {
		return nil
	}
	return append([]string{}, args[argsLenAtDash:]...)
}

var cliWorkingDirOverride string

func setCLIWorkingDirOverride(cwd string) string {
	prev := cliWorkingDirOverride
	cliWorkingDirOverride = strings.TrimSpace(cwd)
	return prev
}

func currentCLIWorkingDir() (string, error) {
	cwd, err := os.Getwd()
	if cliWorkingDirOverride != "" {
		return cliWorkingDirOverride, nil
	}

	return cliWorkingDirFrom(cwd, err, getenvFallback("PWD"), getenvFallback("INIT_CWD"))
}

func cliWorkingDirFrom(cwd string, cwdErr error, pwd, initCwd string) (string, error) {
	pwd = strings.TrimSpace(pwd)
	initCwd = strings.TrimSpace(initCwd)
	if initCwd != "" {
		if cwdErr != nil {
			return initCwd, nil
		}
		if wrapperCwd := currentPackageManagerWrapperDir(cwd, pwd, initCwd); wrapperCwd != "" {
			return wrapperCwd, nil
		}
		if sameCLIPath(pwd, initCwd) {
			if shellCwd := currentShellWorkingDir(cwd, pwd); shellCwd != "" {
				return shellCwd, nil
			}
			if sameCLIPath(pwd, currentPackageManagerRoot()) {
				return cwd, nil
			}
			if pwd != "" {
				return pwd, nil
			}
			return cwd, nil
		}
	}

	if shellCwd := currentShellWorkingDir(cwd, pwd); shellCwd != "" {
		return shellCwd, nil
	}
	if initCwd == "" {
		return cwd, cwdErr
	}
	return cwd, nil
}

func currentShellWorkingDir(cwd, pwd string) string {
	pwd = strings.TrimSpace(pwd)
	if sameCLIPath(pwd, cwd) {
		return pwd
	}
	return ""
}

func currentPackageManagerWrapperDir(cwd, pwd, initCwd string) string {
	if !sameCLIPath(cwd, currentPackageManagerRoot()) {
		return ""
	}
	pwd = strings.TrimSpace(pwd)
	if pwd != "" && !sameCLIPath(pwd, cwd) {
		return ""
	}
	if sameCLIPath(initCwd, cwd) {
		return ""
	}
	return initCwd
}

func currentPackageManagerRoot() string {
	if localPrefix := strings.TrimSpace(getenvFallback("npm_config_local_prefix")); localPrefix != "" {
		return localPrefix
	}
	if packageJSON := strings.TrimSpace(getenvFallback("npm_package_json")); packageJSON != "" {
		return filepath.Dir(packageJSON)
	}
	return ""
}

func resolveCLIWorkingDirOverride(baseCwd, override string) string {
	override = strings.TrimSpace(override)
	if override == "" {
		return ""
	}

	base := strings.TrimSpace(baseCwd)
	if resolvedBase, err := cliWorkingDirFrom(baseCwd, nil, getenvFallback("PWD"), getenvFallback("INIT_CWD")); err == nil {
		base = strings.TrimSpace(resolvedBase)
	}
	if resolved, ok := resolveCLIPath(base, override); ok {
		return resolved
	}
	if filepath.IsAbs(override) {
		return filepath.Clean(override)
	}
	if base == "" {
		return filepath.Clean(override)
	}
	return filepath.Clean(filepath.Join(base, override))
}

func resolveCLIPath(baseCwd, target string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}

	current := strings.TrimSpace(baseCwd)
	if filepath.IsAbs(target) {
		volume := filepath.VolumeName(target)
		current = volume + string(filepath.Separator)
		target = strings.TrimPrefix(target, current)
	} else if current == "" {
		return "", false
	}

	parts := splitCLIPath(target)
	if len(parts) == 0 {
		return filepath.Clean(current), true
	}

	appendedDepth := 0
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if appendedDepth > 0 {
				if resolved, err := filepath.EvalSymlinks(current); err == nil {
					current = resolved
				}
				appendedDepth--
			}
			current = filepath.Dir(current)
		default:
			current = filepath.Join(current, part)
			appendedDepth++
		}
	}

	return filepath.Clean(current), true
}

func splitCLIPath(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
}

func sameCLIPath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	if a == b {
		return true
	}
	return canonicalCLIPath(a) == canonicalCLIPath(b)
}

func canonicalCLIPath(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}

func worktreeIDForMeta(cwd string, meta *sandbox.SandboxMeta) string {
	if meta != nil {
		if worktreeID := strings.TrimSpace(meta.WorktreeID); worktreeID != "" {
			return worktreeID
		}
	}
	return sandbox.ComputeWorktreeID(cwd)
}

func syncOptionsForMeta(cwd string, meta *sandbox.SandboxMeta) sandbox.SyncOptions {
	return sandbox.SyncOptions{
		Cwd:        cwd,
		WorktreeID: worktreeIDForMeta(cwd, meta),
	}
}

func worktreeLogDir(cwd string, meta *sandbox.SandboxMeta) string {
	return "/amux/logs/" + worktreeIDForMeta(cwd, meta)
}

func getenvFallback(keys ...string) string {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok && val != "" {
			return val
		}
	}
	return ""
}
