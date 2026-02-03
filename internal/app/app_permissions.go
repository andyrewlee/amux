package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/permissions"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// initPermissionWatcher creates and registers the permission watcher.
func (a *App) initPermissionWatcher() {
	pw, err := permissions.NewPermissionWatcher(func(root string, newAllow []string) {
		select {
		case a.permWatcherCh <- messages.PermissionWatcherEvent{Root: root, NewAllow: newAllow}:
		default:
		}
	})
	if err != nil {
		logging.Warn("Permission watcher disabled: %v", err)
		return
	}
	a.permissionWatcher = pw
	a.supervisor.Start("permissions.watcher", pw.Run, supervisor.WithBackoff(500*time.Millisecond))
}

// startPermissionWatcher starts listening for permission watcher events.
func (a *App) startPermissionWatcher() tea.Cmd {
	if a.permissionWatcher == nil || a.permWatcherCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.permWatcherCh
	}
}

// watchAllWorkspacePermissions starts watching all known workspace roots.
func (a *App) watchAllWorkspacePermissions() {
	if a.permissionWatcher == nil {
		return
	}
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			_ = a.permissionWatcher.Watch(a.projects[i].Workspaces[j].Root)
		}
	}
}

// unwatchAllWorkspacePermissions stops watching all workspace roots.
func (a *App) unwatchAllWorkspacePermissions() {
	if a.permissionWatcher == nil {
		return
	}
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			a.permissionWatcher.Unwatch(a.projects[i].Workspaces[j].Root)
		}
	}
}

// handlePermissionWatcherEvent processes a permission watcher event.
func (a *App) handlePermissionWatcherEvent(msg messages.PermissionWatcherEvent) []tea.Cmd {
	cmds := []tea.Cmd{a.startPermissionWatcher()}

	// The watcher already tracked what's new since we started watching.
	// Only process if there are actually new permissions.
	if len(msg.NewAllow) == 0 {
		return cmds
	}

	// Normalize the project's settings files to convert legacy formats
	_ = config.NormalizeProjectPermissions(msg.Root)

	// Filter out permissions already in global list
	global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
	if err != nil {
		logging.Warn("Failed to load global permissions: %v", err)
		return cmds
	}

	// Only keep permissions not already in global allow or deny lists
	newPerms := config.DiffPermissions(global.Allow, msg.NewAllow)
	newPerms = config.DiffPermissions(global.Deny, newPerms)
	if len(newPerms) == 0 {
		return cmds
	}

	// Find workspace name for the toast
	wsName := ""
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			if a.projects[i].Workspaces[j].Root == msg.Root {
				wsName = a.projects[i].Workspaces[j].Name
				break
			}
		}
		if wsName != "" {
			break
		}
	}

	cmds = append(cmds, func() tea.Msg {
		return messages.PermissionDetected{
			WorkspaceRoot: msg.Root,
			WorkspaceName: wsName,
			NewAllow:      newPerms,
		}
	})
	return cmds
}

// handlePermissionDetected processes detected permissions.
func (a *App) handlePermissionDetected(msg messages.PermissionDetected) tea.Cmd {
	if a.config.UI.AutoAddPermissions {
		// Auto-add to global allow list
		global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
		if err != nil {
			return a.toast.ShowError("Failed to load global permissions")
		}
		added := 0
		for _, perm := range msg.NewAllow {
			if global.AddAllow(perm) {
				added++
			}
		}
		if added == 0 {
			return nil
		}
		if err := config.SaveGlobalPermissions(a.config.Paths.GlobalPermissionsPath, global); err != nil {
			return a.toast.ShowError("Failed to save global permissions")
		}
		_ = config.InjectIntoAllProfiles(a.config.Paths.ProfilesRoot, global)

		// Normalize the workspace that triggered this to remove legacy formats
		_ = config.NormalizeProjectPermissions(msg.WorkspaceRoot)

		if added == 1 {
			return a.toast.ShowSuccess(fmt.Sprintf("Permission '%s' added to global allow list", msg.NewAllow[0]))
		}
		return a.toast.ShowSuccess(fmt.Sprintf("%d permissions added to global allow list", added))
	}

	// Manual mode: append to pending
	for _, perm := range msg.NewAllow {
		a.pendingPermissions = append(a.pendingPermissions, common.PendingPermission{
			Permission: perm,
			Source:     msg.WorkspaceName,
		})
	}

	if len(msg.NewAllow) == 1 {
		return a.toast.ShowInfo(fmt.Sprintf("Permission '%s' detected. Ctrl-a g to review", msg.NewAllow[0]))
	}
	return a.toast.ShowInfo(fmt.Sprintf("%d new permissions detected. Ctrl-a g to review", len(msg.NewAllow)))
}

// handlePermissionsDialogResult processes the pending permissions dialog result.
func (a *App) handlePermissionsDialogResult(msg messages.PermissionsDialogResult) tea.Cmd {
	a.permissionsDialog = nil

	global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
	if err != nil {
		return a.toast.ShowError("Failed to load global permissions")
	}

	processed := make(map[string]bool)
	for _, action := range msg.Actions {
		processed[action.Permission] = true
		switch action.Action {
		case messages.PermissionAllow:
			global.AddAllow(action.Permission)
		case messages.PermissionDeny:
			global.AddDeny(action.Permission)
		case messages.PermissionSkip:
			// Do nothing
		}
	}

	if err := config.SaveGlobalPermissions(a.config.Paths.GlobalPermissionsPath, global); err != nil {
		return a.toast.ShowError("Failed to save global permissions")
	}
	_ = config.InjectIntoAllProfiles(a.config.Paths.ProfilesRoot, global)

	// Normalize all workspace project settings to remove legacy formats
	a.normalizeAllWorkspaceSettings()

	// Remove processed items from pending
	var remaining []common.PendingPermission
	for _, p := range a.pendingPermissions {
		if !processed[p.Permission] {
			remaining = append(remaining, p)
		}
	}
	a.pendingPermissions = remaining

	return a.toast.ShowSuccess("Global permissions updated")
}

// handlePermissionsEditorResult processes the permissions editor result.
func (a *App) handlePermissionsEditorResult(msg messages.PermissionsEditorResult) tea.Cmd {
	a.permissionsEditor = nil

	if !msg.Confirmed {
		return nil
	}

	global := &config.GlobalPermissions{
		Allow: msg.Allow,
		Deny:  msg.Deny,
	}
	if err := config.SaveGlobalPermissions(a.config.Paths.GlobalPermissionsPath, global); err != nil {
		return a.toast.ShowError("Failed to save global permissions")
	}
	_ = config.InjectIntoAllProfiles(a.config.Paths.ProfilesRoot, global)

	// Normalize all workspace project settings to remove legacy formats
	a.normalizeAllWorkspaceSettings()

	return a.toast.ShowSuccess("Global permissions saved")
}

// normalizeAllWorkspaceSettings normalizes permission formats in all workspace settings.
func (a *App) normalizeAllWorkspaceSettings() {
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			_ = config.NormalizeProjectPermissions(a.projects[i].Workspaces[j].Root)
		}
	}
}
