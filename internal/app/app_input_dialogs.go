package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
	"github.com/andyrewlee/medusa/internal/ui/sidebar"
	"github.com/andyrewlee/medusa/internal/update"
	"github.com/andyrewlee/medusa/internal/validation"
)

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	project := a.dialogProject
	workspace := a.dialogWorkspace
	defaultName := a.dialogDefaultName
	workspaceRoot := a.dialogWorkspaceRoot
	profile := a.dialogProfile
	groupName := a.dialogGroupName
	groupRepos := a.dialogGroupRepos
	group := a.dialogGroup
	groupWs := a.dialogGroupWs
	a.dialog = nil
	a.dialogProject = nil
	a.dialogWorkspace = nil
	a.dialogDefaultName = ""
	a.dialogWorkspaceRoot = ""
	a.dialogProfile = ""
	// Only clear group state for terminal group dialogs
	switch result.ID {
	case DialogAddProject, DialogCreateGroup, DialogAddGroupRepo:
		// Don't clear yet — wizard may continue (unified flow)
	default:
		a.dialogGroupName = ""
		a.dialogGroupRepos = nil
		a.dialogGroup = nil
		a.dialogGroupWs = nil
	}
	logging.Debug("Dialog result: id=%s confirmed=%v value=%s", result.ID, result.Confirmed, result.Value)

	if !result.Confirmed {
		a.pendingProfileLaunch = ""
		a.pendingProfileLaunchRoot = ""
		logging.Debug("Dialog cancelled")
		// If we were adding a new project and the user cancelled profile selection,
		// remove the project since a profile is required.
		if a.pendingNewProjectPath != "" {
			path := a.pendingNewProjectPath
			a.pendingNewProjectPath = ""
			cmds := []tea.Cmd{
				a.removeProjectByPath(path),
			}
			return a.safeBatch(cmds...)
		}
		// Return to profile manager if we were creating/renaming/deleting a profile
		if result.ID == DialogCreateProfile || result.ID == DialogRenameProfile || result.ID == DialogDeleteProfile {
			return func() tea.Msg { return common.ShowProfileManager{} }
		}
		// Cancelled a group wizard step — clean up group state
		if a.dialogGroupName != "" {
			a.dialogGroupName = ""
		}
		return nil
	}

	switch result.ID {
	case DialogAddProject:
		// Unified flow: multi-select picker returns Values
		if len(result.Values) == 1 {
			// Single repo → add as project
			path := validation.SanitizeInput(result.Values[0])
			logging.Info("Adding single project from unified dialog: %s", path)
			if err := validation.ValidateProjectPath(path); err != nil {
				logging.Warn("Project path validation failed: %v", err)
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating project path"}
				}
			}
			return func() tea.Msg {
				return messages.AddProject{Path: path}
			}
		} else if len(result.Values) >= 2 {
			// Multiple repos → create group (show name dialog first)
			logging.Info("Creating group from unified dialog with %d repos", len(result.Values))
			a.dialogGroupRepos = result.Values
			a.dialog = common.NewInputDialog(DialogCreateGroup, "Name Your Workspace", "")
			a.dialog.SetMessage("Enter a name for the workspace.")
			a.dialog.SetInputValidate(func(s string) string {
				s = validation.SanitizeInput(s)
				if s == "" {
					return ""
				}
				if err := validation.ValidateWorkspaceName(s); err != nil {
					return err.Error()
				}
				return ""
			})
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil
		} else if result.Value != "" {
			// Legacy: non-multi-select fallback
			path := validation.SanitizeInput(result.Value)
			logging.Info("Adding project from dialog: %s", path)
			if err := validation.ValidateProjectPath(path); err != nil {
				logging.Warn("Project path validation failed: %v", err)
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating project path"}
				}
			}
			return func() tea.Msg {
				return messages.AddProject{Path: path}
			}
		}

	case DialogCreateWorkspace:
		if project != nil {
			name := validation.SanitizeInput(result.Value)
			if name == "" {
				name = defaultName
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			// Remember checkbox state for next workspace creation
			allowEdits := result.CheckboxValue
			a.config.UI.LastAllowEdits = allowEdits
			_ = a.config.SaveUISettings()
			// Show progress overlay and start async fetch
			a.creationOverlay = common.NewProgressOverlay("Creating Workspace", []string{
				"Fetching latest changes",
				"Creating worktree",
			})
			a.creationOverlay.SetStepDetail(filepath.Base(project.Path))
			a.creationOverlay.SetSize(a.width, a.height)
			return a.fetchRemoteBase(project, name, allowEdits)
		}

	case DialogDeleteWorkspace:
		if project != nil && workspace != nil {
			ws := workspace
			return func() tea.Msg {
				return messages.DeleteWorkspace{
					Project:   project,
					Workspace: ws,
				}
			}
		}

	case DialogRemoveProject:
		if project != nil {
			proj := project
			return func() tea.Msg {
				return messages.RemoveProject{
					Project: proj,
				}
			}
		}

	case DialogSelectAssistant, "agent-picker":
		if a.activeWorkspace != nil {
			assistant := result.Value
			if err := validation.ValidateAssistant(assistant); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating assistant"}
				}
			}
			// Remember the selected agent as the default for future launches
			a.config.UI.DefaultAgent = assistant
			_ = a.config.SaveUISettings()
			ws := a.activeWorkspace
			return func() tea.Msg {
				return messages.LaunchAgent{
					Assistant: assistant,
					Workspace: ws,
				}
			}
		}

	case DialogRenameWorkspace:
		if workspace != nil {
			name := validation.SanitizeInput(result.Value)
			if name == "" || name == workspace.Name {
				return nil // No change
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			ws := workspace
			proj := project
			return func() tea.Msg {
				return messages.RenameWorkspace{Project: proj, Workspace: ws, NewName: name}
			}
		}

	case DialogSetProfile:
		a.pendingNewProjectPath = "" // profile selected (or existing project), clear pending state
		if project != nil {
			if result.Value == common.NewProfileOption {
				// User chose "New profile..." — show the input dialog
				a.dialogProject = project
				a.dialogDefaultName = "Default"
				a.dialog = common.NewInputDialog(DialogSetProfile, "Set Profile", "Default")
				a.dialog.SetMessage("Profile isolates Claude settings (permissions, memory) for this project.")
				a.dialog.SetSize(a.width, a.height)
				a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
				a.dialog.Show()
				return nil
			}
			selectedProfile := result.Value
			if selectedProfile == "" {
				selectedProfile = defaultName
			}
			proj := project
			return func() tea.Msg {
				return messages.SetProfile{
					Project: proj,
					Profile: selectedProfile,
				}
			}
		}

	case DialogRenameProfile:
		if profile != "" {
			newName := validation.SanitizeInput(result.Value)
			if newName == "" || newName == profile {
				return nil // No change
			}
			if err := validation.ValidateProfileName(newName); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating profile name"}
				}
			}
			oldName := profile
			return func() tea.Msg {
				return messages.RenameProfile{OldName: oldName, NewName: newName}
			}
		}

	case DialogCreateProfile:
		name := validation.SanitizeInput(result.Value)
		if name == "" {
			return func() tea.Msg { return common.ShowProfileManager{} }
		}
		if err := validation.ValidateProfileName(name); err != nil {
			return func() tea.Msg {
				return messages.Error{Err: err, Context: "validating profile name"}
			}
		}
		return func() tea.Msg {
			return messages.CreateProfile{Name: name}
		}

	case DialogDeleteProfile:
		if profile != "" {
			p := profile
			return func() tea.Msg {
				return messages.DeleteProfile{Profile: p}
			}
		}

	// --- Group dialog results ---

	case DialogCreateGroup:
		name := validation.SanitizeInput(result.Value)
		if name == "" {
			return nil
		}
		// Group name entered — repos are already in dialogGroupRepos (from unified flow or edit)
		repos := groupRepos
		if len(repos) < 2 {
			a.dialogGroupName = ""
			a.dialogGroupRepos = nil
			return a.toast.ShowError("A group needs at least 2 repos")
		}
		a.dialogGroupName = name

		// Show profile picker
		profiles := a.listProfiles()
		if len(profiles) > 0 {
			a.dialogGroupRepos = repos
			a.dialog = common.NewProfilePicker(DialogSetGroupProfile, profiles, "")
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil
		}
		// No profiles exist — create with default
		a.dialogGroupName = ""
		a.dialogGroupRepos = nil
		return func() tea.Msg {
			return messages.CreateGroup{Name: name, RepoPaths: repos, Profile: "Default"}
		}

	case DialogAddGroupRepo:
		// Used for editing group repos (add/remove flow)
		repos := result.Values
		if len(repos) < 2 {
			return a.toast.ShowError("A group needs at least 2 repos")
		}
		if group != nil {
			grp := group
			return func() tea.Msg {
				return messages.UpdateGroupRepos{Group: grp, RepoPaths: repos}
			}
		}
		return nil

	case DialogCreateGroupWorkspace:
		if group != nil {
			name := validation.SanitizeInput(result.Value)
			if name == "" {
				name = defaultName
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating group workspace name"}
				}
			}
			allowEdits := result.CheckboxValue
			a.config.UI.LastAllowEdits = allowEdits
			_ = a.config.SaveUISettings()
			grpName := group.Name
			return func() tea.Msg {
				return messages.CreateGroupWorkspace{
					GroupName:    grpName,
					Name:         name,
					AllowEdits:   allowEdits,
					LoadClaudeMD: false,
				}
			}
		}

	case DialogDeleteGroup:
		if groupName != "" {
			name := groupName
			return func() tea.Msg {
				return messages.RemoveGroup{Name: name}
			}
		}

	case DialogRenameGroup:
		if group != nil {
			name := validation.SanitizeInput(result.Value)
			if name == "" || name == group.Name {
				return nil
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating group name"}
				}
			}
			g := group
			return func() tea.Msg {
				return messages.RenameGroup{Group: g, NewName: name}
			}
		}

	case DialogRenameGroupWorkspace:
		if group != nil && groupWs != nil {
			name := validation.SanitizeInput(result.Value)
			if name == "" || name == groupWs.Name {
				return nil
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			g := group
			gw := groupWs
			return func() tea.Msg {
				return messages.RenameGroupWorkspace{Group: g, Workspace: gw, NewName: name}
			}
		}

	case DialogDeleteGroupWorkspace:
		if group != nil && groupWs != nil {
			g := group
			gw := groupWs
			return func() tea.Msg {
				return messages.DeleteGroupWorkspace{Group: g, Workspace: gw}
			}
		}

	case DialogSetGroupProfile:
		selectedProfile := result.Value
		if selectedProfile == common.NewProfileOption {
			// User chose "New profile..." — show input dialog
			a.dialog = common.NewInputDialog(DialogSetGroupProfile, "Set Profile", "Default")
			a.dialog.SetMessage("Profile isolates Claude settings for this group.")
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil
		}
		if selectedProfile == "" {
			selectedProfile = "Default"
		}

		// Check if this was from the group creation wizard
		if groupName != "" && len(groupRepos) > 0 {
			name := groupName
			repos := groupRepos
			a.dialogGroupName = ""
			a.dialogGroupRepos = nil
			return func() tea.Msg {
				return messages.CreateGroup{Name: name, RepoPaths: repos, Profile: selectedProfile}
			}
		}

		// Normal group profile update
		if group != nil {
			grpName := group.Name
			return func() tea.Msg {
				return messages.SetGroupProfile{GroupName: grpName, Profile: selectedProfile}
			}
		}

	case DialogQuit:
		// Persist workspace tabs synchronously before shutdown.
		// Shutdown() closes tabs (sets Running=false), so we must
		// capture current state first to avoid saving "stopped" status.
		a.persistAllWorkspacesNow()
		a.Shutdown()
		a.quitting = true
		return tea.Quit

	case DialogCleanupTmux:
		return func() tea.Msg { return messages.CleanupTmuxSessions{} }

	case DialogCommit:
		if workspaceRoot != "" && result.Value != "" {
			message := validation.SanitizeInput(result.Value)
			root := workspaceRoot
			return func() tea.Msg {
				hash, err := git.CreateCommit(root, message)
				return messages.ActionBarCommitResult{
					Success:    err == nil,
					CommitHash: hash,
					Err:        err,
				}
			}
		}
	}

	return nil
}

func (a *App) showQuitDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogQuit,
		"Quit MEDUSA",
		"Are you sure you want to quit?",
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleUpdateCheckComplete handles the UpdateCheckComplete message.
func (a *App) handleUpdateCheckComplete(msg messages.UpdateCheckComplete) tea.Cmd {
	if msg.Err != nil {
		logging.Debug("Update check error: %v", msg.Err)
		return nil
	}
	if !msg.UpdateAvailable {
		logging.Debug("No update available (current=%s, latest=%s)", msg.CurrentVersion, msg.LatestVersion)
		return nil
	}
	// Store update info
	a.updateAvailable = &update.CheckResult{
		CurrentVersion:  msg.CurrentVersion,
		LatestVersion:   msg.LatestVersion,
		UpdateAvailable: msg.UpdateAvailable,
		ReleaseNotes:    msg.ReleaseNotes,
	}
	logging.Info("Update available: %s -> %s", msg.CurrentVersion, msg.LatestVersion)
	// Update settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		a.settingsDialog.SetUpdateInfo(msg.CurrentVersion, msg.LatestVersion, true)
	}
	return nil
}

// handleTriggerUpgrade handles the TriggerUpgrade message.
func (a *App) handleTriggerUpgrade() tea.Cmd {
	if a.updateAvailable == nil || a.upgradeRunning {
		return nil
	}
	a.upgradeRunning = true
	return func() tea.Msg {
		updater := update.NewUpdater(a.version, a.commit, a.buildDate)
		// Get the latest release
		result, err := updater.Check()
		if err != nil {
			return messages.UpgradeComplete{Err: err}
		}
		if result.Release == nil {
			return messages.UpgradeComplete{Err: fmt.Errorf("no release found")}
		}
		// Perform the upgrade
		if err := updater.Upgrade(result.Release); err != nil {
			return messages.UpgradeComplete{Err: err}
		}
		return messages.UpgradeComplete{NewVersion: result.Release.TagName}
	}
}

// handleUpgradeComplete handles the UpgradeComplete message.
func (a *App) handleUpgradeComplete(msg messages.UpgradeComplete) tea.Cmd {
	a.upgradeRunning = false
	if msg.Err != nil {
		logging.Error("Upgrade failed: %v", msg.Err)
		return a.toast.ShowError("Upgrade failed: " + msg.Err.Error())
	}
	a.updateAvailable = nil
	// Update settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		a.settingsDialog.SetUpdateInfo(msg.NewVersion, "", false)
	}
	logging.Info("Upgrade complete: %s", msg.NewVersion)
	return a.toast.ShowSuccess("Upgraded to " + msg.NewVersion + " - restart medusa to use new version")
}

// handleOpenFileInEditor handles the OpenFileInEditor message from the project tree.
// This opens the file in vim in the center pane.
func (a *App) handleOpenFileInEditor(msg sidebar.OpenFileInEditor) tea.Cmd {
	if msg.Workspace == nil || msg.Path == "" {
		return nil
	}
	logging.Info("Opening file in editor: %s", msg.Path)
	newCenter, cmd := a.center.Update(messages.OpenFileInVim{
		Path:      msg.Path,
		Workspace: msg.Workspace,
	})
	a.center = newCenter
	return cmd
}
