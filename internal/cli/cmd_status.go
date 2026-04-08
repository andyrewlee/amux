package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	statusTmuxEnsureAvailable         = tmux.EnsureAvailable
	statusTmuxAmuxSessionsByWorkspace = tmux.AmuxSessionsByWorkspace
	statusTmuxListSessions            = tmux.ListSessions
)

type statusResult struct {
	Version              string `json:"version"`
	TmuxAvailable        bool   `json:"tmux_available"`
	HomeReadable         bool   `json:"home_readable"`
	ProjectCount         int    `json:"project_count"`
	WorkspaceCount       int    `json:"workspace_count"`
	SessionCount         int    `json:"session_count"`
	StackChildCount      int    `json:"stack_child_count"`
	StackRootCount       int    `json:"stack_root_count"`
	ActiveStackRootCount int    `json:"active_stack_root_count"`
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

	svc, code := initServicesOrFail(w, wErr, gf, version)
	if code >= 0 {
		return code
	}

	result := statusResult{Version: svc.Version}

	// Tmux
	result.TmuxAvailable = statusTmuxEnsureAvailable() == nil

	// Home dir
	result.HomeReadable = isReadable(svc.Config.Paths.Home)

	// Projects
	projects, err := svc.Registry.Projects()
	if err == nil {
		result.ProjectCount = len(projects)
	}

	// Workspaces
	wsIDs, err := svc.Store.List()
	if err == nil {
		result.WorkspaceCount = len(wsIDs)
		loadedWorkspaces := make([]*data.Workspace, 0, len(wsIDs))
		for _, id := range wsIDs {
			ws, loadErr := svc.Store.Load(id)
			if loadErr != nil || ws == nil || ws.Archived {
				continue
			}
			loadedWorkspaces = append(loadedWorkspaces, ws)
		}
		stackRoots := make(map[string]bool)
		for _, ws := range loadedWorkspaces {
			if ws == nil || !ws.HasStackParent() {
				continue
			}
			result.StackChildCount++
			stackRoot := string(ws.EffectiveStackRootWorkspaceID())
			if strings.TrimSpace(stackRoot) != "" {
				stackRoots[stackRoot] = true
			}
		}
		result.StackRootCount = len(stackRoots)
		if result.TmuxAvailable {
			activeByWorkspace, activeErr := statusTmuxAmuxSessionsByWorkspace(svc.TmuxOpts)
			if activeErr == nil {
				activeRoots := make(map[string]bool)
				for _, ws := range loadedWorkspaces {
					if ws == nil {
						continue
					}
					if len(activeByWorkspace[string(ws.ID())]) == 0 {
						continue
					}
					stackRoot := string(ws.EffectiveStackRootWorkspaceID())
					if strings.TrimSpace(stackRoot) != "" && stackRoots[stackRoot] {
						activeRoots[stackRoot] = true
					}
				}
				result.ActiveStackRootCount = len(activeRoots)
			}
		}
	}

	// Sessions
	if result.TmuxAvailable {
		sessions, err := statusTmuxListSessions(svc.TmuxOpts)
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
		fmt.Fprintf(w, "  stack kids: %d\n", result.StackChildCount)
		fmt.Fprintf(w, "  stack roots:%d\n", result.StackRootCount)
		fmt.Fprintf(w, "  active stk: %d\n", result.ActiveStackRootCount)
	})
	return ExitOK
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unavailable"
}
