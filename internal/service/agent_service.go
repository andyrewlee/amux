package service

import (
	"fmt"
	"sync"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
	appPty "github.com/andyrewlee/medusa/internal/pty"
	"github.com/andyrewlee/medusa/internal/tmux"
)

// AgentService manages non-Claude agents (Codex, Gemini, Amp, etc.) via PTY/tmux.
// These agents don't support structured JSON output, so we keep the existing
// PTY approach for them.
type AgentService struct {
	config       *config.Config
	agentManager *appPty.AgentManager
	eventBus     *EventBus

	mu   sync.RWMutex
	tabs map[string]*PTYTab // tabID → PTY tab
}

// PTYTab wraps a PTY-based agent tab.
type PTYTab struct {
	Info    TabInfo
	Agent   *appPty.Agent
	Session string // tmux session name

	// Output subscribers
	subMu       sync.RWMutex
	subscribers map[string]chan []byte
}

// NewAgentService creates an agent service for non-Claude agents.
func NewAgentService(cfg *config.Config, bus *EventBus) *AgentService {
	return &AgentService{
		config:       cfg,
		agentManager: appPty.NewAgentManager(cfg),
		eventBus:     bus,
		tabs:         make(map[string]*PTYTab),
	}
}

// LaunchAgent starts a new non-Claude agent in a PTY/tmux session.
func (s *AgentService) LaunchAgent(ws *data.Workspace, agentType string, rows, cols uint16) (string, error) {
	if ws == nil {
		return "", fmt.Errorf("workspace is required")
	}

	tabID := generateTabID()

	sessionName, err := tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())
	if err != nil {
		return "", fmt.Errorf("generating session name: %w", err)
	}

	tags := tmux.SessionTags{
		WorkspaceID: string(ws.ID()),
		TabID:       tabID,
		Type:        agentType,
		Assistant:   agentType,
	}

	agent, err := s.agentManager.CreateAgentWithTags(ws, appPty.AgentType(agentType), sessionName, rows, cols, tags, appPty.AgentOptions{})
	if err != nil {
		return "", fmt.Errorf("creating agent: %w", err)
	}

	tab := &PTYTab{
		Info: TabInfo{
			ID:          tabID,
			WorkspaceID: ws.ID(),
			Kind:        TabKindPTY,
			Assistant:   agentType,
			State:       TabStateRunning,
		},
		Agent:       agent,
		Session:     sessionName,
		subscribers: make(map[string]chan []byte),
	}

	s.mu.Lock()
	s.tabs[tabID] = tab
	s.mu.Unlock()

	// Start reading PTY output
	go s.readPTYOutput(tab)

	s.eventBus.Publish(NewEvent(EventTabCreated, &tab.Info))
	logging.Info("AgentService: launched %s tab %s in %s", agentType, tabID, ws.Root)
	return tabID, nil
}

// SendInput sends raw bytes to a PTY tab's terminal.
func (s *AgentService) SendInput(tabID string, data []byte) error {
	s.mu.RLock()
	tab, ok := s.tabs[tabID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("tab %s not found", tabID)
	}

	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return fmt.Errorf("tab %s has no terminal", tabID)
	}

	_, err := tab.Agent.Terminal.Write(data)
	return err
}

// SendInterrupt sends Ctrl+C to a PTY tab.
func (s *AgentService) SendInterrupt(tabID string) error {
	s.mu.RLock()
	tab, ok := s.tabs[tabID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("tab %s not found", tabID)
	}

	if tab.Agent == nil {
		return fmt.Errorf("tab %s has no agent", tabID)
	}

	return s.agentManager.SendInterrupt(tab.Agent)
}

// ResizeTerminal resizes a PTY tab's terminal.
func (s *AgentService) ResizeTerminal(tabID string, rows, cols uint16) error {
	s.mu.RLock()
	tab, ok := s.tabs[tabID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("tab %s not found", tabID)
	}

	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return fmt.Errorf("tab %s has no terminal", tabID)
	}

	return tab.Agent.Terminal.SetSize(rows, cols)
}

// CloseTab closes a PTY tab and kills its process.
func (s *AgentService) CloseTab(tabID string) error {
	s.mu.Lock()
	tab, ok := s.tabs[tabID]
	if ok {
		delete(s.tabs, tabID)
		tab.Info.State = TabStateClosed
	}
	s.mu.Unlock()

	if !ok {
		return nil
	}

	if tab.Agent != nil {
		_ = s.agentManager.CloseAgent(tab.Agent)
	}

	// Kill tmux session
	if tab.Session != "" {
		_ = tmux.KillSession(tab.Session, tmux.DefaultOptions())
	}

	// Close subscriber channels
	tab.subMu.Lock()
	for id, ch := range tab.subscribers {
		close(ch)
		delete(tab.subscribers, id)
	}
	tab.subMu.Unlock()

	s.eventBus.Publish(NewEvent(EventTabClosed, map[string]string{"tab_id": tabID}))
	return nil
}

// SubscribePTYOutput subscribes to raw PTY output from a tab.
func (s *AgentService) SubscribePTYOutput(tabID string) (<-chan []byte, func()) {
	s.mu.RLock()
	tab, ok := s.tabs[tabID]
	s.mu.RUnlock()

	ch := make(chan []byte, 256)

	if !ok {
		close(ch)
		return ch, func() {}
	}

	subID := generateSubID()
	tab.subMu.Lock()
	tab.subscribers[subID] = ch
	tab.subMu.Unlock()

	unsub := func() {
		tab.subMu.Lock()
		delete(tab.subscribers, subID)
		tab.subMu.Unlock()
	}

	return ch, unsub
}

// GetTabState returns the current state of a PTY tab.
func (s *AgentService) GetTabState(tabID string) (*TabInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tab, ok := s.tabs[tabID]
	if !ok {
		return nil, fmt.Errorf("tab %s not found", tabID)
	}
	return &tab.Info, nil
}

// ListTabs returns all PTY tabs for a workspace.
func (s *AgentService) ListTabs(wsID data.WorkspaceID) []TabInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tabs []TabInfo
	for _, tab := range s.tabs {
		if tab.Info.WorkspaceID == wsID {
			tabs = append(tabs, tab.Info)
		}
	}
	return tabs
}

// Shutdown closes all PTY tabs.
func (s *AgentService) Shutdown() {
	s.mu.Lock()
	tabIDs := make([]string, 0, len(s.tabs))
	for id := range s.tabs {
		tabIDs = append(tabIDs, id)
	}
	s.mu.Unlock()

	for _, id := range tabIDs {
		_ = s.CloseTab(id)
	}
}

// --- internal ---

func (s *AgentService) readPTYOutput(tab *PTYTab) {
	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := tab.Agent.Terminal.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			tab.subMu.RLock()
			for _, ch := range tab.subscribers {
				select {
				case ch <- data:
				default:
					// Drop for slow subscriber
				}
			}
			tab.subMu.RUnlock()
		}

		if err != nil {
			break
		}
	}

	// Terminal closed
	s.mu.Lock()
	if tab.Info.State != TabStateClosed {
		tab.Info.State = TabStateStopped
	}
	s.mu.Unlock()

	// Close subscriber channels
	tab.subMu.Lock()
	for id, ch := range tab.subscribers {
		close(ch)
		delete(tab.subscribers, id)
	}
	tab.subMu.Unlock()

	s.eventBus.Publish(NewEvent(EventTabStateChanged, map[string]any{
		"tab_id": tab.Info.ID,
		"state":  TabStateStopped,
	}))
}
