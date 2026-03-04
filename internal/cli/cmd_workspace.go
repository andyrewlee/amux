package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
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
	const usage = "Usage: amux workspace list [--repo <path>|--project <path>] [--archived] [--all] [--json]"
	fs := newFlagSet("workspace list")
	repo := fs.String("repo", "", "filter by repo path")
	project := fs.String("project", "", "alias for --repo")
	archived := fs.Bool("archived", false, "include archived workspaces")
	all := fs.Bool("all", false, "include unregistered/orphaned workspace metadata")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(*repo) != "" && strings.TrimSpace(*project) != "" {
		return returnUsageError(
			w, wErr, gf, usage, version,
			errors.New("use either --repo or --project, not both"),
		)
	}
	if *all && (strings.TrimSpace(*repo) != "" || strings.TrimSpace(*project) != "") {
		return returnUsageError(
			w, wErr, gf, usage, version,
			errors.New("use either --all or a repo/project filter, not both"),
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
		infos, err = listAll(svc, *archived, *all)
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
	visible, err := isVisibleRegisteredRepo(svc, repoPath)
	if err != nil {
		return nil, err
	}
	if !visible {
		return []WorkspaceInfo{}, nil
	}

	var workspaces []*data.Workspace
	if includeArchived {
		workspaces, err = svc.Store.ListByRepoIncludingArchived(repoPath)
	} else {
		workspaces, err = svc.Store.ListByRepo(repoPath)
	}
	if err != nil {
		return nil, err
	}
	cache := newSymlinkCache()
	infos := make([]WorkspaceInfo, 0, len(workspaces))
	for _, ws := range workspaces {
		if !shouldSurfaceWorkspaceForCLI(svc.Config.Paths.WorkspacesRoot, ws, cache) {
			continue
		}
		infos = append(infos, workspaceToInfo(ws))
	}
	return infos, nil
}

func normalizeRepoPathForCompare(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if canonical, err := canonicalizeProjectPath(path); err == nil {
		return canonical
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func isVisibleRegisteredRepo(svc *Services, repoPath string) (bool, error) {
	target := normalizeRepoPathForCompare(repoPath)
	if target == "" {
		slog.Debug("repo path normalized to empty", "path", repoPath)
		return false, nil
	}

	paths, err := svc.Registry.Projects()
	if err != nil {
		return false, err
	}

	for _, path := range paths {
		if !git.IsGitRepository(path) {
			continue
		}
		if normalizeRepoPathForCompare(path) == target {
			return true, nil
		}
	}
	slog.Debug("repo not in registered projects", "path", repoPath, "normalized", target, "registered", len(paths))
	return false, nil
}

func listAll(svc *Services, includeArchived, includeAll bool) ([]WorkspaceInfo, error) {
	if includeAll {
		return listAllStoredMetadata(svc, includeArchived)
	}
	visibleRepos, err := visibleRegisteredRepoSet(svc)
	if err != nil {
		return nil, err
	}
	if len(visibleRepos) == 0 {
		return []WorkspaceInfo{}, nil
	}

	ids, err := svc.Store.List()
	if err != nil {
		return nil, err
	}
	workspaces := make([]*data.Workspace, 0, len(ids))
	seen := make(map[string]int, len(ids))
	cache := newSymlinkCache()
	loadErrors := 0
	for _, id := range ids {
		ws, err := svc.Store.Load(id)
		if err != nil {
			loadErrors++
			continue
		}
		if !includeArchived && ws.Archived {
			continue
		}
		if !shouldSurfaceWorkspaceForCLI(svc.Config.Paths.WorkspacesRoot, ws, cache) {
			continue
		}
		repoKey := normalizeRepoPathForCompare(ws.Repo)
		if _, ok := visibleRepos[repoKey]; !ok {
			continue
		}
		identity := workspaceIdentityKeyCLI(ws)
		if idx, ok := seen[identity]; ok {
			if shouldPreferWorkspaceCLI(ws, workspaces[idx]) {
				workspaces[idx] = ws
			}
			continue
		}
		seen[identity] = len(workspaces)
		workspaces = append(workspaces, ws)
	}

	if loadErrors > 0 && len(workspaces) == 0 {
		return nil, fmt.Errorf("failed to load %d workspace metadata entries", loadErrors)
	}

	infos := make([]WorkspaceInfo, 0, len(workspaces))
	for _, ws := range workspaces {
		infos = append(infos, workspaceToInfo(ws))
	}
	return infos, nil
}

func visibleRegisteredRepoSet(svc *Services) (map[string]struct{}, error) {
	paths, err := svc.Registry.Projects()
	if err != nil {
		return nil, err
	}
	visibleRepos := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !git.IsGitRepository(path) {
			continue
		}
		key := normalizeRepoPathForCompare(path)
		if key == "" {
			continue
		}
		visibleRepos[key] = struct{}{}
	}
	return visibleRepos, nil
}

func workspaceIdentityKeyCLI(ws *data.Workspace) string {
	if ws == nil {
		return ""
	}
	repoKey := normalizeRepoPathForCompare(ws.Repo)
	rootKey := normalizeRepoPathForCompare(ws.Root)
	if repoKey != "" && rootKey != "" {
		return repoKey + "\n" + rootKey
	}
	return strings.TrimSpace(ws.Repo) + "\n" + strings.TrimSpace(ws.Root)
}

func shouldPreferWorkspaceCLI(candidate, existing *data.Workspace) bool {
	if existing == nil {
		return true
	}
	if candidate == nil {
		return false
	}
	if existing.Archived && !candidate.Archived {
		return true
	}
	if !existing.Archived && candidate.Archived {
		return false
	}
	if existing.Created.IsZero() && !candidate.Created.IsZero() {
		return true
	}
	if !existing.Created.IsZero() && candidate.Created.IsZero() {
		return false
	}
	if candidate.Created.After(existing.Created) {
		return true
	}
	if existing.Created.After(candidate.Created) {
		return false
	}
	if existing.Name == "" && candidate.Name != "" {
		return true
	}
	if quality := workspaceMetadataQualityCLI(candidate) - workspaceMetadataQualityCLI(existing); quality != 0 {
		return quality > 0
	}
	return false
}

func workspaceMetadataQualityCLI(ws *data.Workspace) int {
	if ws == nil {
		return 0
	}
	score := 0
	if strings.TrimSpace(ws.Name) != "" {
		score++
	}
	if strings.TrimSpace(ws.Branch) != "" {
		score++
	}
	if strings.TrimSpace(ws.Base) != "" {
		score++
	}
	if strings.TrimSpace(ws.Assistant) != "" {
		score++
	}
	if strings.TrimSpace(ws.ScriptMode) != "" {
		score++
	}
	if strings.TrimSpace(ws.Runtime) != "" {
		score++
	}
	if len(ws.Env) > 0 {
		score++
	}
	if len(ws.OpenTabs) > 0 {
		score++
	}
	return score
}

func listAllStoredMetadata(svc *Services, includeArchived bool) ([]WorkspaceInfo, error) {
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
