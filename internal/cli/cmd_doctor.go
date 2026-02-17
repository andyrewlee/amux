package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/andyrewlee/amux/internal/tmux"
)

type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, warn, fail
	Message string `json:"message"`
}

type doctorResult struct {
	Checks []checkResult `json:"checks"`
}

func cmdDoctor(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux doctor [--json]"
	if len(args) > 0 {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(args, " ")),
		)
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	var checks []checkResult

	// 1. tmux installed
	checks = append(checks, checkTmuxInstalled())

	// 2. tmux version
	checks = append(checks, checkTmuxVersion())

	// 3. amux home exists and writable
	checks = append(checks, checkHomeDir(svc.Config.Paths.Home))

	// 4. metadata parseable
	checks = append(checks, checkMetadata(svc))

	// 5. registry parseable
	checks = append(checks, checkRegistry(svc))

	// 6. tmux server reachable
	checks = append(checks, checkTmuxServer(svc.TmuxOpts))

	result := doctorResult{Checks: checks}

	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		for _, c := range result.Checks {
			icon := "+"
			if c.Status == "warn" {
				icon = "!"
			} else if c.Status == "fail" {
				icon = "x"
			}
			fmt.Fprintf(w, "  [%s] %-25s %s\n", icon, c.Name, c.Message)
		}
	})
	return ExitOK
}

func checkTmuxInstalled() checkResult {
	if tmux.EnsureAvailable() == nil {
		return checkResult{Name: "tmux_installed", Status: "ok", Message: "tmux found on PATH"}
	}
	return checkResult{Name: "tmux_installed", Status: "fail", Message: "tmux not found; " + tmux.InstallHint()}
}

func checkTmuxVersion() checkResult {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return checkResult{Name: "tmux_version", Status: "fail", Message: "could not determine tmux version"}
	}
	ver := strings.TrimSpace(string(out))
	return checkResult{Name: "tmux_version", Status: "ok", Message: ver}
}

func checkHomeDir(home string) checkResult {
	info, err := os.Stat(home)
	if err != nil {
		return checkResult{Name: "amux_home", Status: "fail", Message: home + " not found"}
	}
	if !info.IsDir() {
		return checkResult{Name: "amux_home", Status: "fail", Message: home + " is not a directory"}
	}
	// Check writable by creating a temp file
	tmp, err := os.CreateTemp(home, ".doctor-check-*")
	if err != nil {
		return checkResult{Name: "amux_home", Status: "warn", Message: home + " is not writable"}
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return checkResult{Name: "amux_home", Status: "ok", Message: home}
}

func checkMetadata(svc *Services) checkResult {
	ids, err := svc.Store.List()
	if err != nil {
		return checkResult{Name: "metadata", Status: "fail", Message: err.Error()}
	}
	return checkResult{Name: "metadata", Status: "ok", Message: fmt.Sprintf("%d workspace(s)", len(ids))}
}

func checkRegistry(svc *Services) checkResult {
	projects, err := svc.Registry.Projects()
	if err != nil {
		return checkResult{Name: "registry", Status: "fail", Message: err.Error()}
	}
	return checkResult{Name: "registry", Status: "ok", Message: fmt.Sprintf("%d project(s)", len(projects))}
}

func checkTmuxServer(opts tmux.Options) checkResult {
	sessions, err := tmux.ListSessions(opts)
	if err != nil {
		return checkResult{Name: "tmux_server", Status: "warn", Message: "server not reachable (no sessions)"}
	}
	return checkResult{Name: "tmux_server", Status: "ok", Message: fmt.Sprintf("%d session(s)", len(sessions))}
}
