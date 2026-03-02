package handlers

import (
	"net/http"

	"github.com/andyrewlee/medusa/internal/service"
)

// ListGroups returns all project groups.
func (h *Handlers) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.svc.Groups.ListGroups()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

// CreateGroup creates a new project group.
func (h *Handlers) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string   `json:"name"`
		Repos   []string `json:"repos"`
		Profile string   `json:"profile,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Groups.CreateGroup(req.Name, req.Repos, req.Profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "name": req.Name})
}

// DeleteGroup removes a project group.
func (h *Handlers) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.svc.Groups.DeleteGroup(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateGroupWorkspace creates a workspace within a group.
func (h *Handlers) CreateGroupWorkspace(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("name")
	var req struct {
		Name       string `json:"name"`
		BranchMode string `json:"branch_mode,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	gw, err := h.svc.Groups.CreateGroupWorkspace(service.GroupWorkspaceOpts{
		GroupName: groupName,
		Name:      req.Name,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, gw)
}

// DeleteGroupWorkspace removes a group workspace.
func (h *Handlers) DeleteGroupWorkspace(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("name")
	wsID := r.PathValue("wsID")
	if err := h.svc.Groups.DeleteGroupWorkspace(groupName, wsID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
