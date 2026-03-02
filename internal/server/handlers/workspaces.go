package handlers

import (
	"net/http"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/service"
)

// GetWorkspace returns workspace details.
func (h *Handlers) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))
	ws, err := h.svc.Workspaces.GetWorkspace(wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

// CreateWorkspace creates a new git worktree workspace.
func (h *Handlers) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectPath     string `json:"project_path"`
		Name            string `json:"name"`
		BranchMode      string `json:"branch_mode,omitempty"`
		CustomBranch    string `json:"custom_branch,omitempty"`
		AllowEdits      bool   `json:"allow_edits,omitempty"`
		Isolated        bool   `json:"isolated,omitempty"`
		SkipPermissions bool   `json:"skip_permissions,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ws, err := h.svc.Workspaces.CreateWorkspace(service.CreateWorkspaceOpts{
		ProjectPath:     req.ProjectPath,
		Name:            req.Name,
		BranchMode:      req.BranchMode,
		CustomBranch:    req.CustomBranch,
		AllowEdits:      req.AllowEdits,
		Isolated:        req.Isolated,
		SkipPermissions: req.SkipPermissions,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

// DeleteWorkspace removes a workspace.
func (h *Handlers) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))
	if err := h.svc.Workspaces.DeleteWorkspace(wsID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RenameWorkspace renames a workspace.
func (h *Handlers) RenameWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Workspaces.RenameWorkspace(wsID, req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ArchiveWorkspace marks a workspace as archived.
func (h *Handlers) ArchiveWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))
	if err := h.svc.Workspaces.ArchiveWorkspace(wsID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetGitStatus returns git status for a workspace.
func (h *Handlers) GetGitStatus(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))
	ws, err := h.svc.Workspaces.GetWorkspace(wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	status, err := h.svc.Projects.GetGitStatus(ws.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}
