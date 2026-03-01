package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/pty"
)

// ClaudeService manages Claude Code processes via structured JSONL I/O.
// Instead of wrapping Claude in a PTY/tmux, it spawns Claude Code with
// --output-format stream-json and --input-format stream-json for a
// structured, device-agnostic interaction.
type ClaudeService struct {
	config   *config.Config
	sessions *SessionStore
	eventBus *EventBus

	mu        sync.RWMutex
	processes map[string]*ClaudeProcess // tabID → running process
	tabs      map[string]*TabInfo       // tabID → tab metadata
}

// ClaudeProcess wraps a running Claude Code subprocess.
type ClaudeProcess struct {
	TabID     string
	SessionID string
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	Cancel    context.CancelFunc
	State     TabState

	// Subscribers receive messages from this process.
	subMu       sync.RWMutex
	subscribers map[string]chan SDKMessage
}

// NewClaudeService creates a Claude service.
func NewClaudeService(cfg *config.Config, sessions *SessionStore, bus *EventBus) *ClaudeService {
	return &ClaudeService{
		config:    cfg,
		sessions:  sessions,
		eventBus:  bus,
		processes: make(map[string]*ClaudeProcess),
		tabs:      make(map[string]*TabInfo),
	}
}

// Init loads existing tab metadata from the session store to identify resumable sessions.
func (s *ClaudeService) Init() error {
	resumable, err := s.sessions.ListResumableSessions()
	if err != nil {
		return fmt.Errorf("listing resumable sessions: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, rs := range resumable {
		if rs.Assistant != "claude" {
			continue
		}
		s.tabs[rs.TabID] = &TabInfo{
			ID:          rs.TabID,
			WorkspaceID: data.WorkspaceID(rs.WorkspaceID),
			Kind:        TabKindClaude,
			Assistant:   "claude",
			State:       TabStateStopped,
			SessionID:   rs.SessionID,
			TotalCost:   rs.TotalCost,
		}
	}

	logging.Info("ClaudeService: loaded %d resumable sessions", len(s.tabs))
	return nil
}

// LaunchAgent starts a new Claude Code process for a workspace.
func (s *ClaudeService) LaunchAgent(ws *data.Workspace, opts LaunchOpts) (string, error) {
	if ws == nil {
		return "", fmt.Errorf("workspace is required")
	}

	tabID := generateTabID()
	sessionID := opts.ResumeSessionID
	isResume := sessionID != ""
	if sessionID == "" {
		sessionID = pty.GenerateSessionID()
	}

	// Build Claude command arguments
	args := []string{
		"--output-format", "stream-json",
		"--include-partial-messages",
	}

	if isResume {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID)
	}

	if opts.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	for _, tool := range opts.AllowedTools {
		args = append(args, "--allowedTools", tool)
	}

	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}

	if opts.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", opts.MaxBudgetUSD))
	}

	// In print mode with initial prompt
	if opts.Prompt != "" && !isResume {
		args = append(args, "-p", opts.Prompt)
	}

	// If no initial prompt and not resuming, use interactive stream-json mode
	if opts.Prompt == "" && !isResume {
		args = append(args, "--input-format", "stream-json")
	}

	// Build environment
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
	)

	// Set CLAUDE_CONFIG_DIR for profiles
	profile := opts.Profile
	if profile == "" {
		profile = ws.Profile
	}
	if profile != "" {
		profileDir := filepath.Join(s.config.Paths.ProfilesRoot, profile)
		_ = os.MkdirAll(profileDir, 0755)

		// Sync plugins if configured
		if s.config.UI.SyncProfilePlugins {
			_ = config.SyncProfileSharedDirs(s.config.Paths.ProfilesRoot, profile)
		}

		// Inject global permissions
		if s.config.UI.GlobalPermissions {
			global, err := config.LoadGlobalPermissions(s.config.Paths.GlobalPermissionsPath)
			if err == nil && (len(global.Allow) > 0 || len(global.Deny) > 0) {
				_ = config.InjectGlobalPermissions(profileDir, global)
			}
		}

		// Inject allow edits
		if opts.AllowEdits || ws.AllowEdits {
			_ = config.InjectAllowEdits(ws.Root)
		}

		env = append(env, fmt.Sprintf("CLAUDE_CONFIG_DIR=%s", profileDir))

		// Pre-trust workspace directory
		_ = config.InjectTrustedDirectory(ws.Root, profileDir)
	}

	// Skip permissions prompt
	if opts.SkipPermissions {
		if profile != "" {
			profileDir := filepath.Join(s.config.Paths.ProfilesRoot, profile)
			_ = config.InjectSkipPermissionPrompt(profileDir)
		}
	}

	// Create the subprocess
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = ws.Root
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	cmd.Stderr = &logWriter{prefix: fmt.Sprintf("[claude:%s] ", tabID[:8])}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("starting claude: %w", err)
	}

	proc := &ClaudeProcess{
		TabID:       tabID,
		SessionID:   sessionID,
		Cmd:         cmd,
		Stdin:       stdin,
		Cancel:      cancel,
		State:       TabStateRunning,
		subscribers: make(map[string]chan SDKMessage),
	}

	tab := &TabInfo{
		ID:          tabID,
		WorkspaceID: ws.ID(),
		Kind:        TabKindClaude,
		Assistant:   "claude",
		State:       TabStateRunning,
		SessionID:   sessionID,
		CreatedAt:   time.Now(),
	}

	s.mu.Lock()
	s.processes[tabID] = proc
	s.tabs[tabID] = tab
	s.mu.Unlock()

	// Record in session store
	_ = s.sessions.SetTabInfo(tabID, string(ws.ID()), "claude", sessionID)

	// Start reading JSONL output
	go s.readLoop(proc, stdout)

	// Publish tab created event
	s.eventBus.Publish(NewEvent(EventTabCreated, tab))

	logging.Info("ClaudeService: launched tab %s (session %s) in %s", tabID, sessionID[:8], ws.Root)
	return tabID, nil
}

// ResumeAgent resumes a previously stopped Claude session.
func (s *ClaudeService) ResumeAgent(tabID string) error {
	s.mu.RLock()
	tab, ok := s.tabs[tabID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("tab %s not found", tabID)
	}

	if tab.State != TabStateStopped {
		return fmt.Errorf("tab %s is not in stopped state (current: %s)", tabID, tab.State)
	}

	if tab.SessionID == "" {
		return fmt.Errorf("tab %s has no session ID to resume", tabID)
	}

	// Look up the workspace
	// For now, we store workspace ID in the tab but need the full workspace to launch
	// TODO: resolve workspace from ID via WorkspaceService dependency
	logging.Info("ClaudeService: resuming tab %s with session %s", tabID, tab.SessionID[:8])

	return nil
}

// SendPrompt sends a user message to an active Claude process.
func (s *ClaudeService) SendPrompt(tabID string, prompt string) error {
	s.mu.RLock()
	proc, ok := s.processes[tabID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no active process for tab %s", tabID)
	}

	if proc.State != TabStateRunning && proc.State != TabStateIdle {
		return fmt.Errorf("tab %s is not accepting input (state: %s)", tabID, proc.State)
	}

	// Send as JSONL to stdin
	msg := map[string]string{
		"type":    "user",
		"content": prompt,
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling prompt: %w", err)
	}

	_, err = fmt.Fprintf(proc.Stdin, "%s\n", line)
	if err != nil {
		return fmt.Errorf("writing to claude stdin: %w", err)
	}

	return nil
}

// Interrupt sends an interrupt signal to stop the current Claude turn.
func (s *ClaudeService) Interrupt(tabID string) error {
	s.mu.RLock()
	proc, ok := s.processes[tabID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no active process for tab %s", tabID)
	}

	if proc.Cmd != nil && proc.Cmd.Process != nil {
		return proc.Cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

// CloseTab closes a tab and kills its process.
func (s *ClaudeService) CloseTab(tabID string) error {
	s.mu.Lock()
	proc, hasProc := s.processes[tabID]
	tab, hasTab := s.tabs[tabID]
	if hasProc {
		delete(s.processes, tabID)
	}
	if hasTab {
		tab.State = TabStateClosed
	}
	s.mu.Unlock()

	if hasProc {
		s.killProcess(proc)
	}

	s.eventBus.Publish(NewEvent(EventTabClosed, map[string]string{"tab_id": tabID}))
	return nil
}

// GetHistory returns the full conversation history for a tab.
func (s *ClaudeService) GetHistory(tabID string) ([]SDKMessage, error) {
	return s.sessions.LoadHistory(tabID)
}

// GetHistorySince returns messages after the specified UUID.
func (s *ClaudeService) GetHistorySince(tabID, lastUUID string) ([]SDKMessage, error) {
	return s.sessions.LoadHistorySince(tabID, lastUUID)
}

// SubscribeMessages subscribes to real-time messages from a Claude tab.
// Returns a channel and an unsubscribe function.
func (s *ClaudeService) SubscribeMessages(tabID string) (<-chan SDKMessage, func()) {
	s.mu.RLock()
	proc, ok := s.processes[tabID]
	s.mu.RUnlock()

	ch := make(chan SDKMessage, 256)

	if !ok {
		// No active process — return closed channel
		close(ch)
		return ch, func() {}
	}

	subID := generateSubID()
	proc.subMu.Lock()
	proc.subscribers[subID] = ch
	proc.subMu.Unlock()

	unsub := func() {
		proc.subMu.Lock()
		delete(proc.subscribers, subID)
		proc.subMu.Unlock()
		// Don't close the channel — caller owns it
	}

	return ch, unsub
}

// GetTabState returns the current state of a tab.
func (s *ClaudeService) GetTabState(tabID string) (*TabInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tab, ok := s.tabs[tabID]
	if !ok {
		return nil, fmt.Errorf("tab %s not found", tabID)
	}
	return tab, nil
}

// ListTabs returns all tabs for a workspace.
func (s *ClaudeService) ListTabs(wsID data.WorkspaceID) []TabInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tabs []TabInfo
	for _, tab := range s.tabs {
		if tab.WorkspaceID == wsID {
			tabs = append(tabs, *tab)
		}
	}
	return tabs
}

// ListAllTabs returns all known tabs.
func (s *ClaudeService) ListAllTabs() []TabInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tabs := make([]TabInfo, 0, len(s.tabs))
	for _, tab := range s.tabs {
		tabs = append(tabs, *tab)
	}
	return tabs
}

// Shutdown gracefully stops all running Claude processes.
func (s *ClaudeService) Shutdown() {
	s.mu.Lock()
	procs := make([]*ClaudeProcess, 0, len(s.processes))
	for _, proc := range s.processes {
		procs = append(procs, proc)
	}
	s.processes = make(map[string]*ClaudeProcess)
	s.mu.Unlock()

	for _, proc := range procs {
		s.killProcess(proc)
	}
}

// --- internal ---

func (s *ClaudeService) readLoop(proc *ClaudeProcess, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg SDKMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			logging.Warn("ClaudeService: malformed JSONL from tab %s: %v", proc.TabID, err)
			continue
		}

		msg.Timestamp = time.Now()

		// Store in session history
		if err := s.sessions.Append(proc.TabID, msg); err != nil {
			logging.Warn("ClaudeService: failed to persist message for tab %s: %v", proc.TabID, err)
		}

		// Update tab metadata
		s.updateTabFromMessage(proc.TabID, &msg)

		// Broadcast to subscribers
		proc.subMu.RLock()
		for _, ch := range proc.subscribers {
			select {
			case ch <- msg:
			default:
				// Drop for slow subscriber
			}
		}
		proc.subMu.RUnlock()

		// Publish to event bus for SSE clients
		s.eventBus.Publish(NewEvent(EventTabMessageReceived, map[string]any{
			"tab_id":  proc.TabID,
			"message": msg,
		}))
	}

	if err := scanner.Err(); err != nil {
		logging.Warn("ClaudeService: read error for tab %s: %v", proc.TabID, err)
	}

	// Process exited
	s.handleProcessExit(proc)
}

func (s *ClaudeService) handleProcessExit(proc *ClaudeProcess) {
	s.mu.Lock()
	delete(s.processes, proc.TabID)
	tab := s.tabs[proc.TabID]
	if tab != nil && tab.State != TabStateClosed {
		tab.State = TabStateStopped
	}
	s.mu.Unlock()

	// Wait for process to fully exit
	if proc.Cmd != nil {
		_ = proc.Cmd.Wait()
	}

	// Close subscriber channels
	proc.subMu.Lock()
	for id, ch := range proc.subscribers {
		close(ch)
		delete(proc.subscribers, id)
	}
	proc.subMu.Unlock()

	s.eventBus.Publish(NewEvent(EventTabStateChanged, map[string]any{
		"tab_id": proc.TabID,
		"state":  TabStateStopped,
	}))

	logging.Info("ClaudeService: process exited for tab %s", proc.TabID)
}

func (s *ClaudeService) updateTabFromMessage(tabID string, msg *SDKMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tab := s.tabs[tabID]
	if tab == nil {
		return
	}

	switch msg.Type {
	case "system":
		if msg.Subtype == "init" {
			if msg.SessionID != "" {
				tab.SessionID = msg.SessionID
			}
			if msg.Model != "" {
				tab.Model = msg.Model
			}
		}
	case "result":
		if msg.TotalCost > 0 {
			tab.TotalCost = msg.TotalCost
		}
		if msg.NumTurns > 0 {
			tab.TurnCount = msg.NumTurns
		}
		tab.State = TabStateIdle
	case "assistant":
		tab.State = TabStateRunning
	}
}

func (s *ClaudeService) killProcess(proc *ClaudeProcess) {
	if proc.Stdin != nil {
		_ = proc.Stdin.Close()
	}
	if proc.Cancel != nil {
		proc.Cancel()
	}
	if proc.Cmd != nil && proc.Cmd.Process != nil {
		_ = proc.Cmd.Process.Kill()
		_ = proc.Cmd.Wait()
	}
}

// logWriter writes lines to the medusa log with a prefix.
type logWriter struct {
	prefix string
}

func (w *logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			logging.Debug("%s%s", w.prefix, line)
		}
	}
	return len(p), nil
}

// --- ID generation ---

var (
	tabCounter int64
	tabMu      sync.Mutex
)

func generateTabID() string {
	tabMu.Lock()
	tabCounter++
	n := tabCounter
	tabMu.Unlock()
	return fmt.Sprintf("tab-%d-%d", time.Now().UnixMilli(), n)
}

func generateSubID() string {
	tabMu.Lock()
	tabCounter++
	n := tabCounter
	tabMu.Unlock()
	return fmt.Sprintf("sub-%d-%d", time.Now().UnixMilli(), n)
}
