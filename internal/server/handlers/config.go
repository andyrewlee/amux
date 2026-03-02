package handlers

import (
	"net/http"

	"github.com/andyrewlee/medusa/internal/config"
)

// GetConfig returns the current UI settings.
func (h *Handlers) GetConfig(w http.ResponseWriter, r *http.Request) {
	settings := h.svc.Config.GetSettings()
	writeJSON(w, http.StatusOK, settings)
}

// UpdateConfig updates the UI settings.
func (h *Handlers) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var settings config.UISettings
	if err := readJSON(r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Config.UpdateSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListProfiles returns all available profile names.
func (h *Handlers) ListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.svc.Config.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if profiles == nil {
		profiles = []string{}
	}
	writeJSON(w, http.StatusOK, profiles)
}

// CreateProfile creates a new named profile.
func (h *Handlers) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Config.CreateProfile(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "name": req.Name})
}

// DeleteProfile removes a profile.
func (h *Handlers) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.svc.Config.DeleteProfile(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetGlobalPermissions returns the global permissions config.
func (h *Handlers) GetGlobalPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.svc.Config.GetGlobalPermissions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, perms)
}

// UpdateGlobalPermissions updates the global permissions config.
func (h *Handlers) UpdateGlobalPermissions(w http.ResponseWriter, r *http.Request) {
	var perms config.GlobalPermissions
	if err := readJSON(r, &perms); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Config.UpdateGlobalPermissions(&perms); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
