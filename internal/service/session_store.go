package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
)

// SessionStore persists conversation messages per tab as append-only JSONL files.
// This enables fast history loading for clients without parsing Claude's internal
// session files. Messages are also kept in memory for active tabs.
type SessionStore struct {
	dir string // ~/.medusa/sessions/

	mu       sync.RWMutex
	metadata map[string]*sessionMeta // tabID → metadata
	// In-memory cache for active tabs. Evicted when tab is closed.
	cache map[string][]SDKMessage // tabID → messages
}

// sessionMeta tracks per-tab metadata stored alongside the JSONL history.
type sessionMeta struct {
	TabID        string    `json:"tab_id"`
	SessionID    string    `json:"session_id"`
	WorkspaceID  string    `json:"workspace_id"`
	Assistant    string    `json:"assistant"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
	TotalCost    float64   `json:"total_cost_usd"`
	MessageCount int       `json:"message_count"`
}

// NewSessionStore creates a session store rooted at the given directory.
func NewSessionStore(dir string) *SessionStore {
	_ = os.MkdirAll(dir, 0755)
	return &SessionStore{
		dir:      dir,
		metadata: make(map[string]*sessionMeta),
		cache:    make(map[string][]SDKMessage),
	}
}

// Init loads metadata for all existing sessions from disk.
func (s *SessionStore) Init() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read sessions dir: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		tabID := entry.Name()[:len(entry.Name())-5] // strip .json
		meta, err := s.loadMetaLocked(tabID)
		if err != nil {
			logging.Warn("Failed to load session metadata for %s: %v", tabID, err)
			continue
		}
		s.metadata[tabID] = meta
	}
	return nil
}

// Append adds a message to a tab's history. It persists to disk and caches in memory.
func (s *SessionStore) Append(tabID string, msg SDKMessage) error {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Append to in-memory cache
	s.cache[tabID] = append(s.cache[tabID], msg)

	// Append to JSONL file
	histPath := s.historyPath(tabID)
	f, err := os.OpenFile(histPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	_, writeErr := fmt.Fprintf(f, "%s\n", line)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write message: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close history file: %w", closeErr)
	}

	// Update metadata
	meta := s.metadata[tabID]
	if meta == nil {
		meta = &sessionMeta{TabID: tabID, CreatedAt: time.Now()}
		s.metadata[tabID] = meta
	}
	meta.LastActivity = time.Now()
	meta.MessageCount++

	// Extract session ID from system init
	if msg.Type == "system" && msg.Subtype == "init" && msg.SessionID != "" {
		meta.SessionID = msg.SessionID
	}
	// Extract cost from result
	if msg.Type == "result" && msg.TotalCost > 0 {
		meta.TotalCost = msg.TotalCost
	}

	return s.saveMetaLocked(tabID, meta)
}

// SetTabInfo records the workspace and assistant for a tab.
func (s *SessionStore) SetTabInfo(tabID, workspaceID, assistant, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta := s.metadata[tabID]
	if meta == nil {
		meta = &sessionMeta{TabID: tabID, CreatedAt: time.Now()}
		s.metadata[tabID] = meta
	}
	meta.WorkspaceID = workspaceID
	meta.Assistant = assistant
	if sessionID != "" {
		meta.SessionID = sessionID
	}
	return s.saveMetaLocked(tabID, meta)
}

// LoadHistory loads the full conversation history for a tab.
// Returns from cache if available, otherwise reads from disk.
func (s *SessionStore) LoadHistory(tabID string) ([]SDKMessage, error) {
	s.mu.RLock()
	if cached, ok := s.cache[tabID]; ok {
		// Return a copy to avoid mutation
		result := make([]SDKMessage, len(cached))
		copy(result, cached)
		s.mu.RUnlock()
		return result, nil
	}
	s.mu.RUnlock()

	// Load from disk
	messages, err := s.loadFromDisk(tabID)
	if err != nil {
		return nil, err
	}

	// Populate cache
	s.mu.Lock()
	s.cache[tabID] = messages
	s.mu.Unlock()

	result := make([]SDKMessage, len(messages))
	copy(result, messages)
	return result, nil
}

// LoadHistorySince returns messages after the specified UUID.
func (s *SessionStore) LoadHistorySince(tabID, lastUUID string) ([]SDKMessage, error) {
	all, err := s.LoadHistory(tabID)
	if err != nil {
		return nil, err
	}

	if lastUUID == "" {
		return all, nil
	}

	for i, msg := range all {
		if msg.UUID == lastUUID {
			if i+1 < len(all) {
				return all[i+1:], nil
			}
			return nil, nil
		}
	}

	// UUID not found, return all messages
	return all, nil
}

// GetSessionID returns the Claude session ID for a tab.
func (s *SessionStore) GetSessionID(tabID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meta := s.metadata[tabID]
	if meta == nil {
		return "", fmt.Errorf("no session found for tab %s", tabID)
	}
	return meta.SessionID, nil
}

// ListResumableSessions returns all sessions that can be resumed.
func (s *SessionStore) ListResumableSessions() ([]ResumableSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ResumableSession
	for _, meta := range s.metadata {
		if meta.SessionID == "" {
			continue
		}
		result = append(result, ResumableSession{
			TabID:        meta.TabID,
			WorkspaceID:  data.WorkspaceID(meta.WorkspaceID),
			SessionID:    meta.SessionID,
			Assistant:    meta.Assistant,
			MessageCount: meta.MessageCount,
			LastActivity: meta.LastActivity,
			TotalCost:    meta.TotalCost,
		})
	}
	return result, nil
}

// EvictCache removes a tab's messages from the in-memory cache.
// The messages remain on disk.
func (s *SessionStore) EvictCache(tabID string) {
	s.mu.Lock()
	delete(s.cache, tabID)
	s.mu.Unlock()
}

// Delete removes all data for a tab (both disk and cache).
func (s *SessionStore) Delete(tabID string) error {
	s.mu.Lock()
	delete(s.cache, tabID)
	delete(s.metadata, tabID)
	s.mu.Unlock()

	_ = os.Remove(s.historyPath(tabID))
	_ = os.Remove(s.metaPath(tabID))
	return nil
}

// --- internal helpers ---

func (s *SessionStore) historyPath(tabID string) string {
	return filepath.Join(s.dir, tabID+".jsonl")
}

func (s *SessionStore) metaPath(tabID string) string {
	return filepath.Join(s.dir, tabID+".json")
}

func (s *SessionStore) loadFromDisk(tabID string) ([]SDKMessage, error) {
	f, err := os.Open(s.historyPath(tabID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open history: %w", err)
	}
	defer f.Close()

	var messages []SDKMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		var msg SDKMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			logging.Warn("Skipping malformed message in %s: %v", tabID, err)
			continue
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return messages, fmt.Errorf("scan history: %w", err)
	}
	return messages, nil
}

func (s *SessionStore) loadMetaLocked(tabID string) (*sessionMeta, error) {
	data, err := os.ReadFile(s.metaPath(tabID))
	if err != nil {
		if os.IsNotExist(err) {
			return &sessionMeta{TabID: tabID}, nil
		}
		return nil, err
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *SessionStore) saveMetaLocked(tabID string, meta *sessionMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(tabID), data, 0644)
}

