package handlers

import (
	"net/http"
	"net/url"
)

// ListProjects returns all registered projects with their workspaces.
func (h *Handlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.svc.Projects.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

// AddProject registers a new git repository.
func (h *Handlers) AddProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	if err := h.svc.Projects.AddProject(req.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "path": req.Path})
}

// RemoveProject unregisters a project.
func (h *Handlers) RemoveProject(w http.ResponseWriter, r *http.Request) {
	path, err := url.PathUnescape(r.PathValue("path"))
	if err != nil || path == "" {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	// Re-add leading / since PathValue strips it
	path = "/" + path

	if err := h.svc.Projects.RemoveProject(path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetProjectProfile sets the Claude profile for a project.
func (h *Handlers) SetProjectProfile(w http.ResponseWriter, r *http.Request) {
	path, err := url.PathUnescape(r.PathValue("path"))
	if err != nil || path == "" {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	path = "/" + path

	var req struct {
		Profile string `json:"profile"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.svc.Projects.SetProfile(path, req.Profile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RescanWorkspaces rediscovers git worktrees for all projects.
func (h *Handlers) RescanWorkspaces(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Projects.RescanWorkspaces(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
