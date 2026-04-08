package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

type workspaceSnapshot struct {
	Workspace  *data.Workspace
	Branch     string
	HeadCommit string
	BaseCommit string
}

type workspaceMutationSpec struct {
	Workspace   *data.Workspace
	Parent      *data.Workspace
	RootBaseRef string
}

type workspaceMutationApplied struct {
	Workspace     *data.Workspace
	OriginalMeta  data.Workspace
	OriginalHead  string
	Rebased       bool
	MetadataSaved bool
}

func activeRepoWorkspaces(store *data.WorkspaceStore, repoPath string) ([]*data.Workspace, error) {
	workspaces, err := store.ListByRepoIncludingArchived(repoPath)
	if err != nil {
		return nil, err
	}
	active := make([]*data.Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws == nil || ws.Archived {
			continue
		}
		active = append(active, ws)
	}
	return active, nil
}

func ensureWorkspaceInList(workspaces []*data.Workspace, ws *data.Workspace) []*data.Workspace {
	if ws == nil {
		return workspaces
	}
	for _, existing := range workspaces {
		if existing != nil && existing.ID() == ws.ID() {
			return workspaces
		}
	}
	return append(workspaces, ws)
}

func workspaceIndex(workspaces []*data.Workspace) map[data.WorkspaceID]*data.Workspace {
	index := make(map[data.WorkspaceID]*data.Workspace, len(workspaces))
	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		index[ws.ID()] = ws
	}
	return index
}

func workspaceChildren(workspaces []*data.Workspace) map[data.WorkspaceID][]*data.Workspace {
	children := make(map[data.WorkspaceID][]*data.Workspace)
	for _, ws := range workspaces {
		if ws == nil || !ws.HasStackParent() {
			continue
		}
		parentID := ws.ParentWorkspaceID
		children[parentID] = append(children[parentID], ws)
	}
	for parentID := range children {
		sort.SliceStable(children[parentID], func(i, j int) bool {
			return data.WorkspaceCreatedDescLess(children[parentID][i], children[parentID][j])
		})
	}
	return children
}

func workspaceSubtree(workspaces []*data.Workspace, rootID data.WorkspaceID) []*data.Workspace {
	index := workspaceIndex(workspaces)
	root := index[rootID]
	if root == nil {
		return nil
	}
	children := workspaceChildren(workspaces)
	ordered := make([]*data.Workspace, 0, len(workspaces))
	var visit func(ws *data.Workspace)
	visit = func(ws *data.Workspace) {
		if ws == nil {
			return
		}
		ordered = append(ordered, ws)
		for _, child := range children[ws.ID()] {
			visit(child)
		}
	}
	visit(root)
	return ordered
}

func descendantSet(workspaces []*data.Workspace, rootID data.WorkspaceID) map[data.WorkspaceID]bool {
	ordered := workspaceSubtree(workspaces, rootID)
	ids := make(map[data.WorkspaceID]bool, len(ordered))
	for _, ws := range ordered {
		if ws == nil {
			continue
		}
		ids[ws.ID()] = true
	}
	return ids
}

func workspaceSnapshotScope(repoWorkspaces, sequence []*data.Workspace, extras ...*data.Workspace) []*data.Workspace {
	scoped := make([]*data.Workspace, 0, len(sequence)+len(extras))
	seen := make(map[data.WorkspaceID]bool, len(sequence)+len(extras))
	index := workspaceIndex(repoWorkspaces)
	add := func(ws *data.Workspace) {
		if ws == nil {
			return
		}
		id := ws.ID()
		if seen[id] {
			return
		}
		seen[id] = true
		scoped = append(scoped, ws)
	}

	for _, ws := range sequence {
		add(ws)
	}
	for _, ws := range sequence {
		if ws == nil || !ws.HasStackParent() {
			continue
		}
		add(index[ws.ParentWorkspaceID])
	}
	for _, ws := range extras {
		add(ws)
	}

	return scoped
}

func resolveRepoBaseRef(repoPath string) string {
	detected, err := git.GetBaseBranch(repoPath)
	return resolveWorkspaceBaseFallback(repoPath, detected, err)
}

func resolveWorkspaceRootBaseRef(repoPath, preferred string) string {
	base := strings.TrimSpace(preferred)
	if base != "" && !strings.EqualFold(base, "HEAD") {
		if gitRefExists(repoPath, base) {
			return base
		}
		trimmed := normalizeRemoteRefToBranch(repoPath, base)
		if !strings.EqualFold(trimmed, "HEAD") && trimmed != base && gitRefExists(repoPath, trimmed) {
			return trimmed
		}
	}
	fallback := resolveRepoBaseRef(repoPath)
	if strings.EqualFold(strings.TrimSpace(fallback), "HEAD") {
		return ""
	}
	return fallback
}

func snapshotWorkspaceStates(workspaces []*data.Workspace) (map[data.WorkspaceID]workspaceSnapshot, error) {
	snapshots := make(map[data.WorkspaceID]workspaceSnapshot, len(workspaces))
	repoBaseRefs := make(map[string]string)
	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		branch, err := git.ResolveCurrentBranchOrFallback(ws.Root, ws.Branch)
		if err != nil {
			return nil, fmt.Errorf("resolve current branch for %s: %w", ws.Name, err)
		}
		headCommit, err := git.ResolveRefCommit(ws.Root, "HEAD")
		if err != nil {
			return nil, fmt.Errorf("resolve HEAD for %s: %w", ws.Name, err)
		}
		snapshots[ws.ID()] = workspaceSnapshot{
			Workspace:  ws,
			Branch:     branch,
			HeadCommit: headCommit,
		}
	}

	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		snapshot := snapshots[ws.ID()]
		if snapshot.BaseCommit != "" {
			continue
		}
		if storedBaseCommit, ok := resolveStoredWorkspaceBaseCommit(ws.Root, ws.BaseCommit); ok {
			snapshot.BaseCommit = storedBaseCommit
			snapshots[ws.ID()] = snapshot
			continue
		}
		if ws.HasStackParent() {
			parentSnapshot, ok := snapshots[ws.ParentWorkspaceID]
			if ok && strings.TrimSpace(parentSnapshot.HeadCommit) != "" {
				baseCommit, err := git.MergeBase(ws.Root, snapshot.HeadCommit, parentSnapshot.HeadCommit)
				if err != nil {
					return nil, fmt.Errorf("derive base commit for %s: %w", ws.Name, err)
				}
				snapshot.BaseCommit = baseCommit
				snapshots[ws.ID()] = snapshot
				continue
			}
			return nil, fmt.Errorf("workspace %s stack parent %s is missing; cannot derive base commit", ws.Name, ws.ParentWorkspaceID)
		}
		repoBaseRef := repoBaseRefs[ws.Repo]
		if repoBaseRef == "" {
			repoBaseRef = resolveRepoBaseRef(ws.Repo)
			repoBaseRefs[ws.Repo] = repoBaseRef
		}
		baseRef := resolveWorkspaceRootBaseRef(ws.Repo, ws.Base)
		if strings.TrimSpace(baseRef) == "" {
			baseRef = repoBaseRef
		}
		baseHead, err := git.ResolveRefCommit(ws.Repo, baseRef)
		if err != nil {
			return nil, fmt.Errorf("resolve base ref %s for %s: %w", baseRef, ws.Name, err)
		}
		baseCommit, err := git.MergeBase(ws.Root, snapshot.HeadCommit, baseHead)
		if err != nil {
			return nil, fmt.Errorf("derive base commit for %s: %w", ws.Name, err)
		}
		snapshot.BaseCommit = baseCommit
		snapshots[ws.ID()] = snapshot
	}

	return snapshots, nil
}

func resolveStoredWorkspaceBaseCommit(workspaceRoot, storedBaseCommit string) (string, bool) {
	storedBaseCommit = strings.TrimSpace(storedBaseCommit)
	if storedBaseCommit == "" {
		return "", false
	}
	resolved, err := git.ResolveRefCommit(workspaceRoot, storedBaseCommit)
	if err != nil {
		return "", false
	}
	resolved = strings.TrimSpace(resolved)
	if resolved == "" {
		return "", false
	}
	return resolved, true
}

func ensureWorkspacesClean(workspaces []*data.Workspace) error {
	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		status, err := git.GetStatusFast(ws.Root)
		if err != nil {
			return fmt.Errorf("read git status for %s: %w", ws.Name, err)
		}
		if !status.Clean {
			return fmt.Errorf("workspace %s (%s) has uncommitted changes", ws.Name, ws.ID())
		}
	}
	return nil
}

func clearWorkspaceParent(ws *data.Workspace) {
	if ws == nil {
		return
	}
	ws.ParentWorkspaceID = ""
	ws.ParentBranch = ""
	ws.StackRootWorkspaceID = ""
	ws.StackDepth = 0
}

func executeWorkspaceMutationPlan(
	svc *Services,
	specs []workspaceMutationSpec,
	snapshots map[data.WorkspaceID]workspaceSnapshot,
) (updated []*data.Workspace, err error) {
	currentHeads := make(map[data.WorkspaceID]string, len(snapshots))
	currentBranches := make(map[data.WorkspaceID]string, len(snapshots))
	for id, snapshot := range snapshots {
		currentHeads[id] = snapshot.HeadCommit
		currentBranches[id] = snapshot.Branch
	}

	applied := make([]workspaceMutationApplied, 0, len(specs))
	defer func() {
		if err == nil {
			return
		}
		if rollbackErr := rollbackWorkspaceMutationPlan(svc, applied); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback failed: %w", rollbackErr))
		}
	}()

	updated = make([]*data.Workspace, 0, len(specs))
	for _, spec := range specs {
		ws := spec.Workspace
		if ws == nil {
			continue
		}
		snapshot, ok := snapshots[ws.ID()]
		if !ok {
			return updated, fmt.Errorf("missing workspace snapshot for %s", ws.Name)
		}
		applied = append(applied, workspaceMutationApplied{
			Workspace:    ws,
			OriginalMeta: *ws,
			OriginalHead: snapshot.HeadCommit,
		})
		appliedIdx := len(applied) - 1

		var (
			newBaseRef    string
			newBaseCommit string
		)
		if spec.Parent != nil {
			parentID := spec.Parent.ID()
			newBaseRef = strings.TrimSpace(currentBranches[parentID])
			newBaseCommit = strings.TrimSpace(currentHeads[parentID])
			if newBaseRef == "" || newBaseCommit == "" {
				return updated, fmt.Errorf("missing parent branch state for %s", spec.Parent.Name)
			}
		} else {
			newBaseRef = resolveWorkspaceRootBaseRef(ws.Repo, spec.RootBaseRef)
			var err error
			newBaseCommit, err = git.ResolveRefCommit(ws.Repo, newBaseRef)
			if err != nil {
				return updated, fmt.Errorf("resolve base ref %s for %s: %w", newBaseRef, ws.Name, err)
			}
		}

		oldBaseCommit := strings.TrimSpace(snapshot.BaseCommit)
		if oldBaseCommit == "" {
			return updated, fmt.Errorf("missing base commit for %s", ws.Name)
		}
		if oldBaseCommit != newBaseCommit {
			if err := git.RebaseCurrentBranchOnto(ws.Root, newBaseCommit, oldBaseCommit); err != nil {
				return updated, fmt.Errorf("rebase %s (%s): %w", ws.Name, ws.ID(), err)
			}
			applied[appliedIdx].Rebased = true
			ws.PendingForcePush = true
		}

		currentHead, err := git.ResolveRefCommit(ws.Root, "HEAD")
		if err != nil {
			return updated, fmt.Errorf("resolve new HEAD for %s: %w", ws.Name, err)
		}
		currentBranch, err := git.ResolveCurrentBranchOrFallback(ws.Root, currentBranches[ws.ID()])
		if err != nil {
			return updated, fmt.Errorf("resolve new branch for %s: %w", ws.Name, err)
		}

		currentHeads[ws.ID()] = currentHead
		currentBranches[ws.ID()] = currentBranch
		ws.Branch = currentBranch
		ws.Base = newBaseRef
		ws.BaseCommit = newBaseCommit
		if spec.Parent != nil {
			data.ApplyStackParent(ws, spec.Parent, newBaseRef)
		} else {
			clearWorkspaceParent(ws)
		}
		updated = append(updated, ws)
	}

	for i := range applied {
		if err := svc.Store.Save(applied[i].Workspace); err != nil {
			return updated, fmt.Errorf("save metadata for %s: %w", applied[i].Workspace.Name, err)
		}
		applied[i].MetadataSaved = true
	}

	return updated, nil
}

func rollbackWorkspaceMutationPlan(svc *Services, applied []workspaceMutationApplied) error {
	if svc == nil || len(applied) == 0 {
		return nil
	}

	errs := make([]string, 0)
	for i := len(applied) - 1; i >= 0; i-- {
		state := applied[i]
		if state.Workspace == nil {
			continue
		}
		if err := git.AbortRebaseIfInProgress(state.Workspace.Root); err != nil {
			errs = append(errs, fmt.Sprintf("abort rebase for %s: %v", state.Workspace.Name, err))
		}
		if state.Rebased {
			if err := git.ResetCurrentBranchHard(state.Workspace.Root, state.OriginalHead); err != nil {
				errs = append(errs, fmt.Sprintf("reset %s to %s: %v", state.Workspace.Name, state.OriginalHead, err))
			}
		}

		*state.Workspace = state.OriginalMeta
		if state.MetadataSaved {
			if err := svc.Store.Save(state.Workspace); err != nil {
				errs = append(errs, fmt.Sprintf("restore metadata for %s: %v", state.Workspace.Name, err))
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

func normalizeRemoteRefToBranch(repoPath, ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if strings.HasPrefix(ref, "refs/remotes/") {
		trimmed := strings.TrimPrefix(ref, "refs/remotes/")
		if _, branch, ok := strings.Cut(trimmed, "/"); ok {
			return branch
		}
		return trimmed
	}
	remoteNames, err := gitRemoteNames(repoPath)
	if err != nil {
		return ref
	}
	for _, remote := range remoteNames {
		prefix := strings.TrimSpace(remote) + "/"
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

func gitRemoteNames(repoPath string) ([]string, error) {
	out, err := git.RunGitCtx(context.Background(), repoPath, "remote")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}
