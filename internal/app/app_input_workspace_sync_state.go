package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/sandbox"
)

type pendingSandboxSync struct {
	source    data.Workspace
	target    data.Workspace
	lookupIDs []string
	inFlight  bool
}

func (a *App) trackPendingSandboxSync(source, target *data.Workspace) {
	if source == nil || target == nil {
		return
	}
	a.storePendingSandboxSync(a.pendingSandboxSyncKeys(source, target), *source, *target)
}

func (a *App) storePendingSandboxSync(keys []string, source, target data.Workspace) {
	if len(keys) == 0 {
		return
	}
	if a.pendingSandboxSyncs == nil {
		a.pendingSandboxSyncs = make(map[string]pendingSandboxSync)
	}
	req := pendingSandboxSync{
		source:    source,
		target:    target,
		lookupIDs: append([]string(nil), keys...),
	}
	for _, key := range keys {
		a.pendingSandboxSyncs[key] = req
	}
}

func (a *App) clearPendingSandboxSync(workspaceID string) {
	if a.pendingSandboxSyncs == nil || strings.TrimSpace(workspaceID) == "" {
		return
	}
	origID := workspaceID
	req, ok := a.pendingSandboxSyncs[workspaceID]
	if !ok {
		workspaceID = a.resolveReboundWorkspaceID(workspaceID)
		req, ok = a.pendingSandboxSyncs[workspaceID]
	}
	if !ok {
		delete(a.pendingSandboxSyncs, origID)
		return
	}
	for _, key := range a.pendingSandboxSyncLookupKeys(req) {
		delete(a.pendingSandboxSyncs, key)
	}
}

func (a *App) retryPendingSandboxSync(workspaceID string) tea.Cmd {
	if a.pendingSandboxSyncs == nil || strings.TrimSpace(workspaceID) == "" {
		return nil
	}
	key := strings.TrimSpace(workspaceID)
	req, ok := a.pendingSandboxSyncs[key]
	if !ok {
		key = a.resolveReboundWorkspaceID(workspaceID)
		req, ok = a.pendingSandboxSyncs[key]
	}
	if !ok {
		return nil
	}
	if req.inFlight {
		return nil
	}
	req.inFlight = true
	for _, alias := range a.pendingSandboxSyncLookupKeys(req) {
		a.pendingSandboxSyncs[alias] = req
	}
	return a.syncWorkspaceFromSandboxWithOptions(&req.source, &req.target, false)
}

func (a *App) pendingSandboxSyncLookupKeys(req pendingSandboxSync) []string {
	keys := req.lookupIDs
	if len(keys) == 0 {
		keys = a.pendingSandboxSyncKeys(&req.source, &req.target)
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func (a *App) pendingSandboxSyncKeys(source, target *data.Workspace) []string {
	seen := make(map[string]struct{}, 4)
	keys := make([]string, 0, 4)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		keys = append(keys, id)
	}
	if source != nil {
		add(string(source.ID()))
		add(a.resolveReboundWorkspaceID(string(source.ID())))
	}
	if target != nil {
		add(string(target.ID()))
		add(a.resolveReboundWorkspaceID(string(target.ID())))
	}
	return keys
}

func (a *App) rememberReboundWorkspaceID(oldID, newID string) {
	oldID = strings.TrimSpace(oldID)
	newID = strings.TrimSpace(newID)
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if a.reboundWorkspaceIDs == nil {
		a.reboundWorkspaceIDs = make(map[string]string)
	}
	for sourceID, targetID := range a.reboundWorkspaceIDs {
		if targetID == oldID {
			a.reboundWorkspaceIDs[sourceID] = newID
		}
	}
	a.reboundWorkspaceIDs[oldID] = newID
}

func (a *App) retargetPendingSandboxSyncs(oldID string, current *data.Workspace) {
	oldID = strings.TrimSpace(oldID)
	if oldID == "" || current == nil || a.pendingSandboxSyncs == nil {
		return
	}
	newID := strings.TrimSpace(string(current.ID()))
	if newID == "" || newID == oldID {
		return
	}

	rebuilt := make(map[string]pendingSandboxSync, len(a.pendingSandboxSyncs))
	seen := make(map[string]struct{}, len(a.pendingSandboxSyncs))
	for _, req := range a.pendingSandboxSyncs {
		previousSource := req.source
		previousTarget := req.target
		updated := false
		if strings.TrimSpace(string(req.source.ID())) == oldID {
			req.source = *current
			updated = true
		}
		if strings.TrimSpace(string(req.target.ID())) == oldID {
			req.target = *current
			updated = true
		}
		if updated && a.sandboxManager != nil {
			from := &previousTarget
			if strings.TrimSpace(string(previousTarget.ID())) != oldID {
				from = &previousSource
			}
			if err := a.sandboxManager.PersistPendingSyncTarget(from, current); err != nil {
				logging.Warn("retarget sandbox sync persist failed for %s -> %s: %v", from.Root, current.Root, err)
			}
		}
		sig := pendingSandboxSyncSignature(req)
		if _, ok := seen[sig]; ok {
			continue
		}
		seen[sig] = struct{}{}
		req.lookupIDs = appendPendingSandboxSyncKeys(req.lookupIDs, a.pendingSandboxSyncKeys(&req.source, &req.target))
		for _, key := range a.pendingSandboxSyncLookupKeys(req) {
			rebuilt[key] = req
		}
	}
	a.pendingSandboxSyncs = rebuilt
}

func appendPendingSandboxSyncKeys(existing, additional []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additional))
	merged := make([]string, 0, len(existing)+len(additional))
	for _, keys := range [][]string{existing, additional} {
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, key)
		}
	}
	return merged
}

func pendingSandboxSyncSignature(req pendingSandboxSync) string {
	return strings.TrimSpace(string(req.source.ID())) + "\x00" +
		strings.TrimSpace(req.source.Root) + "\x00" +
		strings.TrimSpace(string(req.target.ID())) + "\x00" +
		strings.TrimSpace(req.target.Root)
}

func (a *App) resolveReboundWorkspaceID(workspaceID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || a.reboundWorkspaceIDs == nil {
		return workspaceID
	}
	seen := make(map[string]struct{}, len(a.reboundWorkspaceIDs))
	current := workspaceID
	for {
		if _, ok := seen[current]; ok {
			return current
		}
		seen[current] = struct{}{}
		next, ok := a.reboundWorkspaceIDs[current]
		if !ok || strings.TrimSpace(next) == "" || next == current {
			return current
		}
		current = strings.TrimSpace(next)
	}
}

func (a *App) recoverPersistedPendingSandboxSyncs(skipWorkspaceIDs map[string]struct{}) []tea.Cmd {
	if a.sandboxManager == nil {
		return nil
	}
	var cmds []tea.Cmd
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			workspace := &a.projects[i].Workspaces[j]
			if _, skip := skipWorkspaceIDs[string(workspace.ID())]; skip {
				continue
			}
			if data.NormalizeRuntime(workspace.Runtime) == data.RuntimeCloudSandbox {
				continue
			}
			meta, err := loadSandboxMeta(workspace.Root, selectedSandboxMetadataProvider())
			if err != nil || meta == nil || !sandbox.MetaNeedsSync(meta, false) {
				continue
			}
			req := pendingSandboxSync{
				source:    *workspace,
				target:    *workspace,
				lookupIDs: a.pendingSandboxSyncKeys(workspace, workspace),
			}
			if _, ok := a.pendingSandboxSyncs[string(workspace.ID())]; !ok {
				a.storePendingSandboxSync(req.lookupIDs, req.source, req.target)
			}
			if cmd := a.retryPendingSandboxSync(string(workspace.ID())); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return cmds
}
