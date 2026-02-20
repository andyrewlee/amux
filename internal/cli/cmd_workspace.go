package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// WorkspaceInfo is the JSON-serializable workspace representation.
type WorkspaceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	Repo      string `json:"repo"`
	Root      string `json:"root"`
	Runtime   string `json:"runtime"`
	Assistant string `json:"assistant"`
	Archived  bool   `json:"archived"`
	Created   string `json:"created"`
	TabCount  int    `json:"tab_count"`
}

func workspaceToInfo(ws *data.Workspace) WorkspaceInfo {
	created := ""
	if !ws.Created.IsZero() {
		created = ws.Created.UTC().Format("2006-01-02T15:04:05Z")
	}
	return WorkspaceInfo{
		ID:        string(ws.ID()),
		Name:      ws.Name,
		Branch:    ws.Branch,
		Base:      ws.Base,
		Repo:      ws.Repo,
		Root:      ws.Root,
		Runtime:   ws.Runtime,
		Assistant: ws.Assistant,
		Archived:  ws.Archived,
		Created:   created,
		TabCount:  len(ws.OpenTabs),
	}
}

func cmdWorkspaceList(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace list [--repo <path>|--project <path>] [--archived] [--json]"
	fs := newFlagSet("workspace list")
	repo := fs.String("repo", "", "filter by repo path")
	project := fs.String("project", "", "alias for --repo")
	archived := fs.Bool("archived", false, "include archived workspaces")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(*repo) != "" && strings.TrimSpace(*project) != "" {
		return returnUsageError(
			w, wErr, gf, usage, version,
			errors.New("use either --repo or --project, not both"),
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

	var infos []WorkspaceInfo

	repoFilter := strings.TrimSpace(*repo)
	if repoFilter == "" {
		repoFilter = strings.TrimSpace(*project)
	}
	if repoFilter != "" {
		repoPath := repoFilter
		if canonical, cErr := canonicalizeProjectPath(repoPath); cErr == nil {
			repoPath = canonical
		} else if abs, aErr := filepath.Abs(repoPath); aErr == nil {
			repoPath = abs
		}
		infos, err = listByRepo(svc, repoPath, *archived)
	} else {
		infos, err = listAll(svc, *archived)
	}
	if err != nil {
		if gf.JSON {
			ReturnError(w, "list_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "%v", err)
		}
		return ExitInternalError
	}

	if gf.JSON {
		PrintJSON(w, infos, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		if len(infos) == 0 {
			fmt.Fprintln(w, "No workspaces found.")
			return
		}
		for _, info := range infos {
			status := ""
			if info.Archived {
				status = " (archived)"
			}
			fmt.Fprintf(w, "  %-16s %-20s %-20s %s%s\n",
				info.ID, info.Name, info.Branch, info.Repo, status)
		}
	})
	return ExitOK
}

func listByRepo(svc *Services, repoPath string, includeArchived bool) ([]WorkspaceInfo, error) {
	var workspaces []*data.Workspace
	var err error
	if includeArchived {
		workspaces, err = svc.Store.ListByRepoIncludingArchived(repoPath)
	} else {
		workspaces, err = svc.Store.ListByRepo(repoPath)
	}
	if err != nil {
		return nil, err
	}
	infos := make([]WorkspaceInfo, 0, len(workspaces))
	for _, ws := range workspaces {
		infos = append(infos, workspaceToInfo(ws))
	}
	return infos, nil
}

func listAll(svc *Services, includeArchived bool) ([]WorkspaceInfo, error) {
	ids, err := svc.Store.List()
	if err != nil {
		return nil, err
	}
	infos := make([]WorkspaceInfo, 0, len(ids))
	for _, id := range ids {
		ws, err := svc.Store.Load(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load workspace metadata %s: %w", id, err)
		}
		if !includeArchived && ws.Archived {
			continue
		}
		infos = append(infos, workspaceToInfo(ws))
	}
	return infos, nil
}
