package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/server/handlers"
	"github.com/andyrewlee/medusa/internal/server/middleware"
	"github.com/andyrewlee/medusa/internal/service"
)

// Config holds server configuration.
type Config struct {
	Port     int
	Bind     string
	TLSCert  string
	TLSKey   string
	TokenDir string // directory to store server_token file
	WebAssets fs.FS  // embedded web UI assets (optional)
}

// Server is the Medusa HTTP server.
type Server struct {
	config   Config
	services *service.Services
	server   *http.Server
	token    string
	mu       sync.Mutex
}

// New creates a new Medusa server.
func New(cfg Config, svc *service.Services) (*Server, error) {
	s := &Server{
		config:   cfg,
		services: svc,
	}

	// Load or generate auth token
	token, err := s.loadOrCreateToken()
	if err != nil {
		return nil, fmt.Errorf("initializing auth token: %w", err)
	}
	s.token = token

	return s, nil
}

// Token returns the server's auth token.
func (s *Server) Token() string {
	return s.token
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Apply middleware stack
	var handler http.Handler = mux
	handler = middleware.CORS(handler)
	handler = middleware.Logging(handler)

	addr := fmt.Sprintf("%s:%d", s.config.Bind, s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	scheme := "http"
	if s.config.TLSCert != "" {
		scheme = "https"
	}
	logging.Info("Medusa server listening on %s://%s", scheme, addr)
	logging.Info("Auth token: %s", s.token)

	if s.config.TLSCert != "" && s.config.TLSKey != "" {
		return s.server.ServeTLS(ln, s.config.TLSCert, s.config.TLSKey)
	}
	return s.server.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	logging.Info("Shutting down server...")
	s.services.Shutdown()
	return s.server.Shutdown(ctx)
}

// registerRoutes sets up all HTTP routes.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	auth := middleware.NewAuth(s.token)
	h := handlers.New(s.services)

	// Health check (no auth)
	mux.HandleFunc("GET /api/v1/health", h.Health)

	// All API routes require auth
	api := http.NewServeMux()

	// Projects
	api.HandleFunc("GET /api/v1/projects", h.ListProjects)
	api.HandleFunc("POST /api/v1/projects", h.AddProject)
	api.HandleFunc("DELETE /api/v1/projects/{path...}", h.RemoveProject)
	api.HandleFunc("PUT /api/v1/projects/{path...}/profile", h.SetProjectProfile)
	api.HandleFunc("POST /api/v1/projects/rescan", h.RescanWorkspaces)

	// Workspaces
	api.HandleFunc("GET /api/v1/workspaces/{wsID}", h.GetWorkspace)
	api.HandleFunc("POST /api/v1/workspaces", h.CreateWorkspace)
	api.HandleFunc("DELETE /api/v1/workspaces/{wsID}", h.DeleteWorkspace)
	api.HandleFunc("PUT /api/v1/workspaces/{wsID}/name", h.RenameWorkspace)
	api.HandleFunc("POST /api/v1/workspaces/{wsID}/archive", h.ArchiveWorkspace)
	api.HandleFunc("GET /api/v1/workspaces/{wsID}/git/status", h.GetGitStatus)

	// Tabs (Claude structured)
	api.HandleFunc("GET /api/v1/workspaces/{wsID}/tabs", h.ListTabs)
	api.HandleFunc("POST /api/v1/workspaces/{wsID}/tabs", h.LaunchTab)
	api.HandleFunc("DELETE /api/v1/tabs/{tabID}", h.CloseTab)
	api.HandleFunc("POST /api/v1/tabs/{tabID}/resume", h.ResumeTab)
	api.HandleFunc("POST /api/v1/tabs/{tabID}/interrupt", h.InterruptTab)
	api.HandleFunc("POST /api/v1/tabs/{tabID}/prompt", h.SendPrompt)
	api.HandleFunc("GET /api/v1/tabs/{tabID}/history", h.GetTabHistory)
	api.HandleFunc("GET /api/v1/tabs/{tabID}/state", h.GetTabState)

	// WebSocket endpoints (auth via query param)
	api.HandleFunc("GET /api/v1/tabs/{tabID}/ws", h.TabWebSocket)
	api.HandleFunc("GET /api/v1/tabs/{tabID}/pty", h.TabPTYWebSocket)

	// SSE events
	api.HandleFunc("GET /api/v1/events", h.SSEEvents)

	// Config
	api.HandleFunc("GET /api/v1/config", h.GetConfig)
	api.HandleFunc("PUT /api/v1/config", h.UpdateConfig)
	api.HandleFunc("GET /api/v1/profiles", h.ListProfiles)
	api.HandleFunc("POST /api/v1/profiles", h.CreateProfile)
	api.HandleFunc("DELETE /api/v1/profiles/{name}", h.DeleteProfile)
	api.HandleFunc("GET /api/v1/permissions/global", h.GetGlobalPermissions)
	api.HandleFunc("PUT /api/v1/permissions/global", h.UpdateGlobalPermissions)

	// Groups
	api.HandleFunc("GET /api/v1/groups", h.ListGroups)
	api.HandleFunc("POST /api/v1/groups", h.CreateGroup)
	api.HandleFunc("DELETE /api/v1/groups/{name}", h.DeleteGroup)
	api.HandleFunc("POST /api/v1/groups/{name}/workspaces", h.CreateGroupWorkspace)
	api.HandleFunc("DELETE /api/v1/groups/{name}/workspaces/{wsID}", h.DeleteGroupWorkspace)

	// Wrap API routes with auth
	mux.Handle("/api/", auth.Wrap(api))

	// Static file serving for web UI
	if s.config.WebAssets != nil {
		fileServer := http.FileServer(http.FS(s.config.WebAssets))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			// Try to serve the file; fall back to index.html for SPA routing
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			if _, err := fs.Stat(s.config.WebAssets, path); err != nil {
				// SPA fallback: serve index.html for unknown paths
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><body>
				<h1>Medusa Server</h1>
				<p>Web UI not embedded. Build with: cd web && npm run build</p>
				<p>API: <a href="/api/v1/health">/api/v1/health</a></p>
			</body></html>`)
		})
	}
}

// loadOrCreateToken reads the auth token from disk, generating one if needed.
func (s *Server) loadOrCreateToken() (string, error) {
	tokenPath := filepath.Join(s.config.TokenDir, "server_token")

	data, err := os.ReadFile(tokenPath)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if token != "" {
			return token, nil
		}
	}

	// Generate new token
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	token := "mds_" + hex.EncodeToString(bytes)

	if err := os.MkdirAll(s.config.TokenDir, 0700); err != nil {
		return "", fmt.Errorf("creating token directory: %w", err)
	}
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("writing token: %w", err)
	}

	return token, nil
}
