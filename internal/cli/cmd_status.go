package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/tmux"
)

type statusResult struct {
	Version        string `json:"version"`
	TmuxAvailable  bool   `json:"tmux_available"`
	HomeReadable   bool   `json:"home_readable"`
	ProjectCount   int    `json:"project_count"`
	WorkspaceCount int    `json:"workspace_count"`
	SessionCount   int    `json:"session_count"`
}

func cmdStatus(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux status [--json]"
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

	result := statusResult{Version: svc.Version}

	// Tmux
	result.TmuxAvailable = tmux.EnsureAvailable() == nil

	// Home dir
	result.HomeReadable = isReadable(svc.Config.Paths.Home)

	// Projects (align with visible dashboard/CLI defaults: registered git repos only)
	projects, err := svc.Registry.Projects()
	if err == nil {
		seen := make(map[string]struct{}, len(projects))
		for _, path := range projects {
			if !git.IsGitRepository(path) {
				continue
			}
			key := normalizeRepoPathForCompare(path)
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			result.ProjectCount++
		}
	}

	// Workspaces (same visibility as `workspace list` default)
	workspaces, err := listAll(svc, false, false)
	if err == nil {
		result.WorkspaceCount = len(workspaces)
	}

	// Sessions
	if result.TmuxAvailable {
		sessions, err := tmux.ListSessions(svc.TmuxOpts)
		if err == nil {
			result.SessionCount = len(sessions)
		}
	}

	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "amux %s\n", result.Version)
		fmt.Fprintf(w, "  tmux:       %s\n", boolStatus(result.TmuxAvailable))
		fmt.Fprintf(w, "  home:       %s\n", boolStatus(result.HomeReadable))
		fmt.Fprintf(w, "  projects:   %d\n", result.ProjectCount)
		fmt.Fprintf(w, "  workspaces: %d\n", result.WorkspaceCount)
		fmt.Fprintf(w, "  sessions:   %d\n", result.SessionCount)
	})
	return ExitOK
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unavailable"
}
