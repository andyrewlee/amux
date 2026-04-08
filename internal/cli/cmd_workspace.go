package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// WorkspaceInfo is the JSON-serializable workspace representation.
type WorkspaceInfo struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Branch               string `json:"branch"`
	Base                 string `json:"base"`
	BaseCommit           string `json:"base_commit,omitempty"`
	Repo                 string `json:"repo"`
	Root                 string `json:"root"`
	ParentWorkspaceID    string `json:"parent_workspace_id,omitempty"`
	ParentBranch         string `json:"parent_branch,omitempty"`
	StackRootWorkspaceID string `json:"stack_root_workspace_id,omitempty"`
	StackDepth           int    `json:"stack_depth,omitempty"`
	Runtime              string `json:"runtime"`
	Assistant            string `json:"assistant"`
	Archived             bool   `json:"archived"`
	Created              string `json:"created"`
	TabCount             int    `json:"tab_count"`
}

func workspaceToInfo(ws *data.Workspace) WorkspaceInfo {
	created := ""
	if !ws.Created.IsZero() {
		created = ws.Created.UTC().Format("2006-01-02T15:04:05Z")
	}
	return WorkspaceInfo{
		ID:                   string(ws.ID()),
		Name:                 ws.Name,
		Branch:               ws.Branch,
		Base:                 ws.Base,
		BaseCommit:           ws.BaseCommit,
		Repo:                 ws.Repo,
		Root:                 ws.Root,
		ParentWorkspaceID:    string(ws.ParentWorkspaceID),
		ParentBranch:         ws.ParentBranch,
		StackRootWorkspaceID: string(ws.StackRootWorkspaceID),
		StackDepth:           ws.StackDepth,
		Runtime:              ws.Runtime,
		Assistant:            ws.Assistant,
		Archived:             ws.Archived,
		Created:              created,
		TabCount:             len(ws.OpenTabs),
	}
}

func cmdWorkspaceList(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux workspace list [--repo <path>|--project <path>] [--archived] [--tree] [--json]"
	fs := newFlagSet("workspace list")
	repo := fs.String("repo", "", "filter by repo path")
	project := fs.String("project", "", "alias for --repo")
	archived := fs.Bool("archived", false, "include archived workspaces")
	tree := fs.Bool("tree", false, "render workspaces as a stack tree")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(*repo) != "" && strings.TrimSpace(*project) != "" {
		return returnUsageError(
			w, wErr, gf, usage, version,
			errors.New("use either --repo or --project, not both"),
		)
	}

	svc, code := initServicesOrFail(w, wErr, gf, version)
	if code >= 0 {
		return code
	}

	var infos []WorkspaceInfo
	var err error

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
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "list_failed", err, nil,
			"%v", err)
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
		if *tree {
			renderWorkspaceTree(w, infos)
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

func renderWorkspaceTree(w io.Writer, infos []WorkspaceInfo) {
	byRepo := make(map[string][]WorkspaceInfo)
	repos := make([]string, 0, len(infos))
	for _, info := range infos {
		repo := strings.TrimSpace(info.Repo)
		if _, ok := byRepo[repo]; !ok {
			repos = append(repos, repo)
		}
		byRepo[repo] = append(byRepo[repo], info)
	}
	sort.Strings(repos)

	for idx, repo := range repos {
		if idx > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, repo)
		entries := data.FlattenWorkspaceTree(workspaceInfoSliceToWorkspaces(byRepo[repo]), data.WorkspaceCreatedDescLess)
		for _, entry := range entries {
			ws := entry.Workspace
			if ws == nil {
				continue
			}
			prefix := "  - "
			if entry.Depth > 0 {
				prefix = "  " + strings.Repeat("  ", entry.Depth-1) + "|- "
			}
			fmt.Fprintf(w, "%s%s [%s] %s\n", prefix, ws.Name, ws.Branch, ws.ID())
		}
	}
}

func workspaceInfoSliceToWorkspaces(infos []WorkspaceInfo) []*data.Workspace {
	workspaces := make([]*data.Workspace, 0, len(infos))
	for _, info := range infos {
		ws := &data.Workspace{
			Name:                 info.Name,
			Branch:               info.Branch,
			Base:                 info.Base,
			BaseCommit:           info.BaseCommit,
			Repo:                 info.Repo,
			Root:                 info.Root,
			ParentWorkspaceID:    data.WorkspaceID(strings.TrimSpace(info.ParentWorkspaceID)),
			ParentBranch:         info.ParentBranch,
			StackRootWorkspaceID: data.WorkspaceID(strings.TrimSpace(info.StackRootWorkspaceID)),
			StackDepth:           info.StackDepth,
		}
		if created, err := time.Parse(time.RFC3339, info.Created); err == nil {
			ws.Created = created
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces
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
