package handlers

import (
	"net/http"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/service"
)

// ListTabs returns all tabs for a workspace.
func (h *Handlers) ListTabs(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))
	tabs := h.svc.Claude.ListTabs(wsID)
	writeJSON(w, http.StatusOK, tabs)
}

// LaunchTab starts a new agent tab.
func (h *Handlers) LaunchTab(w http.ResponseWriter, r *http.Request) {
	wsID := data.WorkspaceID(r.PathValue("wsID"))

	var req struct {
		Assistant       string   `json:"assistant"`
		Prompt          string   `json:"prompt,omitempty"`
		SkipPermissions bool     `json:"skip_permissions,omitempty"`
		AllowedTools    []string `json:"allowed_tools,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ws, err := h.svc.Workspaces.GetWorkspace(wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	tabID, err := h.svc.Claude.LaunchAgent(ws, service.LaunchOpts{
		Prompt:          req.Prompt,
		SkipPermissions: req.SkipPermissions,
		AllowedTools:    req.AllowedTools,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"tab_id": tabID})
}

// CloseTab closes a running tab.
func (h *Handlers) CloseTab(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")
	if err := h.svc.Claude.CloseTab(tabID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ResumeTab resumes a stopped session.
func (h *Handlers) ResumeTab(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")
	if err := h.svc.Claude.ResumeAgent(tabID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// InterruptTab interrupts the current turn.
func (h *Handlers) InterruptTab(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")
	if err := h.svc.Claude.Interrupt(tabID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SendPrompt sends a user message to a tab.
func (h *Handlers) SendPrompt(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	if err := h.svc.Claude.SendPrompt(tabID, req.Text); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetTabHistory returns the full conversation history.
func (h *Handlers) GetTabHistory(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")
	sinceUUID := r.URL.Query().Get("since")

	var (
		msgs []service.SDKMessage
		err  error
	)
	if sinceUUID != "" {
		msgs, err = h.svc.Sessions.LoadHistorySince(tabID, sinceUUID)
	} else {
		msgs, err = h.svc.Sessions.LoadHistory(tabID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil {
		msgs = []service.SDKMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GetTabState returns the current tab state.
func (h *Handlers) GetTabState(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")
	state, err := h.svc.Claude.GetTabState(tabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}
