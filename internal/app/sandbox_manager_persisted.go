package app

import (
	"errors"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/sandbox"
)

func (m *SandboxManager) loadPersistedSession(wt *data.Workspace) (*sandboxSession, error) {
	if wt == nil {
		return nil, errors.New("workspace is required")
	}
	meta, err := loadSandboxMeta(wt.Root, selectedSandboxMetadataProvider())
	if err != nil || meta == nil || strings.TrimSpace(meta.SandboxID) == "" {
		return nil, err
	}
	session := m.hydrateSessionFromMeta(nil, wt, meta)
	if err := m.discoverTrackedTmuxSessions(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (m *SandboxManager) hydrateSessionFromMeta(session *sandboxSession, wt *data.Workspace, meta *sandbox.SandboxMeta) *sandboxSession {
	if session == nil {
		session = &sandboxSession{}
	}
	if wt == nil {
		return session
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	providerName := ""
	needsSync := true
	aliases := workspaceIDAliasSet(nil)
	if meta != nil {
		if persisted := strings.TrimSpace(meta.WorktreeID); persisted != "" {
			worktreeID = persisted
		}
		providerName = strings.TrimSpace(meta.Provider)
		needsSync = sandbox.MetaNeedsSync(meta, true)
		aliases = workspaceIDAliasSet(meta.WorkspaceIDs)
	}

	m.mu.Lock()
	session.worktreeID = worktreeID
	session.workspaceRepo = wt.Repo
	session.workspaceRoot = wt.Root
	if providerName != "" {
		session.providerName = providerName
	}
	session.needsSyncDown = needsSync
	if session.workspaceIDAliases == nil {
		session.workspaceIDAliases = make(map[string]struct{})
	}
	for id := range aliases {
		session.workspaceIDAliases[id] = struct{}{}
	}
	m.mu.Unlock()

	m.rememberSessionWorkspaceID(session, wt.ID())
	return session
}

func (m *SandboxManager) setSessionNeedsSync(session *sandboxSession, needsSync bool) {
	if session == nil {
		return
	}
	m.mu.Lock()
	session.needsSyncDown = needsSync
	providerName := session.providerName
	workspaceRoot := session.workspaceRoot
	m.mu.Unlock()
	if strings.TrimSpace(workspaceRoot) == "" {
		return
	}
	if err := sandbox.SetSandboxMetaNeedsSync(workspaceRoot, providerName, needsSync); err != nil {
		logging.Warn("Sandbox metadata sync-state update failed for %s: %v", workspaceRoot, err)
	}
	if err := sandbox.SetSandboxMetaWorkspaceIDs(workspaceRoot, providerName, m.sessionWorkspaceIDs(session)); err != nil {
		logging.Warn("Sandbox metadata workspace-id update failed for %s: %v", workspaceRoot, err)
	}
}

func (m *SandboxManager) refreshSessionWorkspace(session *sandboxSession, wt *data.Workspace) {
	if session == nil || wt == nil {
		return
	}
	m.retargetSessionWorkspace(session, wt)
	m.rememberSessionWorkspaceID(session, wt.ID())
}

func (m *SandboxManager) rebindWorkspace(previous, current *data.Workspace) {
	if err := m.PersistPendingSyncTarget(previous, current); err != nil {
		logging.Warn("Sandbox metadata rebind failed for %s -> %s: %v", previous.Root, current.Root, err)
	}
}

func (m *SandboxManager) PersistPendingSyncTarget(previous, current *data.Workspace) error {
	if previous == nil || current == nil {
		return nil
	}
	if session := m.sessionForWorkspace(previous); session != nil {
		m.refreshSessionWorkspace(session, current)
		return nil
	}

	meta, err := loadSandboxMeta(previous.Root, selectedSandboxMetadataProvider())
	if err != nil {
		return err
	}
	if meta == nil || strings.TrimSpace(meta.SandboxID) == "" {
		return errors.New("sandbox metadata not found")
	}

	providerName := strings.TrimSpace(meta.Provider)
	if previous.Root != current.Root {
		if err := sandbox.MoveSandboxMeta(previous.Root, current.Root, providerName); err != nil {
			return err
		}
	}

	aliases := workspaceIDAliasSet(meta.WorkspaceIDs)
	if aliases == nil {
		aliases = make(map[string]struct{})
	}
	if id := strings.TrimSpace(string(previous.ID())); id != "" {
		aliases[id] = struct{}{}
	}
	if id := strings.TrimSpace(string(current.ID())); id != "" {
		aliases[id] = struct{}{}
	}
	if err := sandbox.SetSandboxMetaWorkspaceIDs(current.Root, providerName, workspaceIDAliasSlice(aliases)); err != nil {
		return err
	}
	return nil
}

func (m *SandboxManager) retargetSessionWorkspace(session *sandboxSession, wt *data.Workspace) {
	if session == nil || wt == nil {
		return
	}
	m.mu.Lock()
	oldRoot := session.workspaceRoot
	providerName := session.providerName
	session.workspaceRepo = wt.Repo
	session.workspaceRoot = wt.Root
	if session.workspaceIDAliases == nil {
		session.workspaceIDAliases = make(map[string]struct{})
	}
	if id := strings.TrimSpace(string(wt.ID())); id != "" {
		session.workspaceID = wt.ID()
		session.workspaceIDAliases[id] = struct{}{}
	}
	workspaceRoot := session.workspaceRoot
	workspaceIDs := sessionWorkspaceIDsLocked(session)
	m.mu.Unlock()
	if oldRoot != "" && oldRoot != wt.Root {
		if err := sandbox.MoveSandboxMeta(oldRoot, wt.Root, providerName); err != nil {
			logging.Warn("Sandbox metadata rekey failed for %s -> %s: %v", oldRoot, wt.Root, err)
		}
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return
	}
	if err := sandbox.SetSandboxMetaWorkspaceIDs(workspaceRoot, providerName, workspaceIDs); err != nil {
		logging.Warn("Sandbox metadata workspace-id update failed for %s: %v", workspaceRoot, err)
	}
}

func (m *SandboxManager) rememberSessionWorkspaceID(session *sandboxSession, workspaceID data.WorkspaceID) {
	if session == nil {
		return
	}
	id := strings.TrimSpace(string(workspaceID))
	m.mu.Lock()
	if session.workspaceIDAliases == nil {
		session.workspaceIDAliases = make(map[string]struct{})
	}
	if id != "" {
		session.workspaceID = workspaceID
		session.workspaceIDAliases[id] = struct{}{}
	}
	providerName := session.providerName
	workspaceRoot := session.workspaceRoot
	workspaceIDs := sessionWorkspaceIDsLocked(session)
	m.mu.Unlock()
	if strings.TrimSpace(workspaceRoot) == "" {
		return
	}
	if err := sandbox.SetSandboxMetaWorkspaceIDs(workspaceRoot, providerName, workspaceIDs); err != nil {
		logging.Warn("Sandbox metadata workspace-id update failed for %s: %v", workspaceRoot, err)
	}
}

func (m *SandboxManager) sessionWorkspaceIDs(session *sandboxSession) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return sessionWorkspaceIDsLocked(session)
}

func sessionWorkspaceIDsLocked(session *sandboxSession) []string {
	if session == nil {
		return nil
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0, len(session.workspaceIDAliases)+1)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(string(session.workspaceID))
	for id := range session.workspaceIDAliases {
		add(id)
	}
	return ids
}

func workspaceIDAliasSet(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	aliases := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		aliases[id] = struct{}{}
	}
	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

func workspaceIDAliasSlice(aliases map[string]struct{}) []string {
	if len(aliases) == 0 {
		return nil
	}
	ids := make([]string, 0, len(aliases))
	for id := range aliases {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}
