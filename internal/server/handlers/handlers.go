package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/andyrewlee/medusa/internal/service"
)

// Handlers holds all HTTP handler methods.
type Handlers struct {
	svc *service.Services
}

// New creates handlers backed by the given services.
func New(svc *service.Services) *Handlers {
	return &Handlers{svc: svc}
}

// Health returns server status.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON marshals v and writes it as JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// readJSON decodes the request body into v.
func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
