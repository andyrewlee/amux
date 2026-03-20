package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

func (m *SandboxManager) SyncToLocal(wt *data.Workspace) error {
	return m.SyncToLocalFrom(wt, wt)
}

func (m *SandboxManager) SyncToLocalFrom(source, target *data.Workspace) error {
	if source == nil {
		source = target
	}
	if target == nil {
		target = source
	}
	if source == nil || target == nil {
		return nil
	}

	session, err := m.attachSession(source)
	if err != nil || session == nil {
		return err
	}
	m.retargetSessionWorkspace(session, target)
	if !session.needsSyncDown {
		return nil
	}
	live, err := m.sessionHasLiveProcesses(session)
	if err != nil {
		return err
	}
	if live {
		return errSandboxSyncLive
	}
	if err := ensureLocalSyncSafe(target.Root); err != nil {
		return err
	}
	if err := m.downloadWorkspace(session.sandbox, sandbox.SyncOptions{
		Cwd:        target.Root,
		WorktreeID: session.worktreeID,
	}, false); err != nil {
		return err
	}
	m.setSessionNeedsSync(session, false)
	return nil
}

func (m *SandboxManager) SyncAllToLocal() error {
	m.mu.Lock()
	sessions := make([]*sandboxSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()

	var errs []string
	for _, session := range sessions {
		if session == nil || !session.needsSyncDown {
			continue
		}
		live, err := m.sessionHasLiveProcesses(session)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", session.workspaceRoot, err))
			continue
		}
		if live {
			continue
		}
		if err := ensureLocalSyncSafe(session.workspaceRoot); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", session.workspaceRoot, err))
			continue
		}
		attached := session
		if attached.sandbox == nil || strings.TrimSpace(attached.workspacePath) == "" {
			repo := attached.workspaceRepo
			if strings.TrimSpace(repo) == "" {
				repo = attached.workspaceRoot
			}
			wt := &data.Workspace{
				Name: string(attached.workspaceID),
				Repo: repo,
				Root: attached.workspaceRoot,
			}
			attached, err = m.attachSession(wt)
			if err != nil || attached == nil {
				if err == nil {
					err = errors.New("sandbox metadata not found")
				}
				errs = append(errs, fmt.Sprintf("%s: %v", session.workspaceRoot, err))
				continue
			}
		}
		if err := m.downloadWorkspace(attached.sandbox, sandbox.SyncOptions{
			Cwd:        session.workspaceRoot,
			WorktreeID: session.worktreeID,
		}, false); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", session.workspaceRoot, err))
			continue
		}
		m.setSessionNeedsSync(attached, false)
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("sandbox sync-down failed: %s", strings.Join(errs, "; "))
}

func ensureLocalSyncSafe(workspaceRoot string) error {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil
	}
	if safe, err := nonGitTargetSyncSafe(root); err != nil {
		return err
	} else if safe {
		return nil
	}
	status, err := git.GetStatusFast(root)
	if err != nil {
		return err
	}
	if status == nil || status.Clean {
		return nil
	}
	return fmt.Errorf("%w: %s", errSandboxSyncConflict, root)
}

func (m *SandboxManager) trackTmuxSession(session *sandboxSession, sessionName string) {
	if session == nil {
		return
	}
	name := strings.TrimSpace(sessionName)
	if name == "" {
		return
	}
	m.mu.Lock()
	if session.tmuxSessionNames == nil {
		session.tmuxSessionNames = make(map[string]struct{})
	}
	session.tmuxSessionNames[name] = struct{}{}
	m.mu.Unlock()
}

func (m *SandboxManager) trackShellAttach(session *sandboxSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	session.activeShells++
	m.mu.Unlock()
}

func (m *SandboxManager) trackShellDetach(session *sandboxSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	if session.activeShells > 0 {
		session.activeShells--
	}
	m.mu.Unlock()
}

func (m *SandboxManager) notifyShellDetached(session *sandboxSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	workspaceID := strings.TrimSpace(string(session.workspaceID))
	callback := m.shellDetachedFn
	m.mu.Unlock()
	if callback == nil || workspaceID == "" {
		return
	}
	callback(workspaceID)
}

func (m *SandboxManager) sessionHasLiveProcesses(session *sandboxSession) (bool, error) {
	if session == nil {
		return false, nil
	}
	if err := m.discoverTrackedTmuxSessions(session); err != nil {
		return false, err
	}
	m.mu.Lock()
	names := make([]string, 0, len(session.tmuxSessionNames))
	for name := range session.tmuxSessionNames {
		names = append(names, name)
	}
	activeShells := session.activeShells
	opts := m.tmuxOptions
	stateFor := m.sessionStateFor
	m.mu.Unlock()

	if activeShells > 0 {
		return true, nil
	}
	for _, name := range names {
		state, err := stateFor(name, opts)
		if err != nil {
			return false, fmt.Errorf("tmux session check failed for %s: %w", name, err)
		}
		if state.Exists && state.HasLivePane {
			return true, nil
		}
	}
	return false, nil
}

func (m *SandboxManager) discoverTrackedTmuxSessions(session *sandboxSession) error {
	if session == nil {
		return nil
	}
	m.mu.Lock()
	workspaceIDs := sessionWorkspaceIDsLocked(session)
	opts := m.tmuxOptions
	sessionsWithTags := m.sessionsWithTags
	m.mu.Unlock()
	if len(workspaceIDs) == 0 || sessionsWithTags == nil {
		return nil
	}

	rows, err := sessionsWithTags(map[string]string{
		"@amux": "1",
	}, []string{"@amux_workspace", "@amux_runtime", "@amux_type"}, opts)
	if err != nil {
		return err
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if !isTrackedSandboxTmuxSession(name, row.Tags, workspaceIDs) {
			continue
		}
		m.trackTmuxSession(session, name)
	}
	return nil
}

func (m *SandboxManager) StopTrackedTmuxSessions() error {
	m.mu.Lock()
	sessions := make([]*sandboxSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()

	var errs []string
	for _, session := range sessions {
		if session == nil {
			continue
		}
		names, err := m.ownedTrackedTmuxSessionNames(session)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", session.workspaceRoot, err))
			continue
		}
		m.mu.Lock()
		opts := m.tmuxOptions
		stateFor := m.sessionStateFor
		killSession := m.killTmuxSession
		m.mu.Unlock()

		for _, name := range names {
			state, err := stateFor(name, opts)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s/%s: %v", session.workspaceRoot, name, err))
				continue
			}
			if !state.Exists {
				continue
			}
			if err := killSession(name, opts); err != nil {
				errs = append(errs, fmt.Sprintf("%s/%s: %v", session.workspaceRoot, name, err))
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("sandbox tmux shutdown failed: %s", strings.Join(errs, "; "))
}

func (m *SandboxManager) ownedTrackedTmuxSessionNames(session *sandboxSession) ([]string, error) {
	if session == nil {
		return nil, nil
	}
	m.mu.Lock()
	workspaceIDs := sessionWorkspaceIDsLocked(session)
	instanceID := strings.TrimSpace(m.instanceID)
	opts := m.tmuxOptions
	sessionsWithTags := m.sessionsWithTags
	m.mu.Unlock()
	if len(workspaceIDs) == 0 || instanceID == "" || sessionsWithTags == nil {
		return nil, nil
	}

	seen := make(map[string]struct{})
	names := make([]string, 0)
	rows, err := sessionsWithTags(map[string]string{
		"@amux":              "1",
		tmux.TagSessionOwner: instanceID,
	}, []string{"@amux_workspace", "@amux_runtime", "@amux_type"}, opts)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if !isTrackedSandboxTmuxSession(name, row.Tags, workspaceIDs) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func isTrackedSandboxTmuxSession(name string, tags map[string]string, workspaceIDs []string) bool {
	if !matchesTrackedSandboxWorkspace(tags["@amux_workspace"], name, workspaceIDs) {
		return false
	}
	runtime := strings.TrimSpace(tags["@amux_runtime"])
	if runtime != "" {
		return runtime == string(data.RuntimeCloudSandbox)
	}
	return strings.HasPrefix(name, sandboxTmuxNamespace+"-")
}

func matchesTrackedSandboxWorkspace(taggedWorkspaceID, sessionName string, workspaceIDs []string) bool {
	taggedWorkspaceID = strings.TrimSpace(taggedWorkspaceID)
	if taggedWorkspaceID != "" {
		for _, workspaceID := range workspaceIDs {
			if taggedWorkspaceID == strings.TrimSpace(workspaceID) {
				return true
			}
		}
	}
	sessionWorkspaceID := strings.TrimSpace(activity.WorkspaceIDFromSessionName(sessionName))
	if sessionWorkspaceID == "" {
		return false
	}
	for _, workspaceID := range workspaceIDs {
		if sessionWorkspaceID == strings.TrimSpace(workspaceID) {
			return true
		}
	}
	return false
}

func nonGitTargetSyncSafe(root string) (bool, error) {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w: %s", errSandboxSyncConflict, root)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return true, nil
	}
	return false, fmt.Errorf("%w: %s", errSandboxSyncConflict, root)
}
