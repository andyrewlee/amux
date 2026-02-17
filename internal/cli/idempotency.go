package cli

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/config"
)

const (
	idempotencyStateFilename = "cli-idempotency.json"
	idempotencyStateVersion  = 1
	idempotencyRetention     = 7 * 24 * time.Hour
)

type idempotencyEntry struct {
	Command   string `json:"command"`
	Key       string `json:"key"`
	ExitCode  int    `json:"exit_code"`
	Envelope  []byte `json:"envelope"`
	CreatedAt int64  `json:"created_at"`
}

type idempotencyState struct {
	Version int                         `json:"version"`
	Entries map[string]idempotencyEntry `json:"entries"`
}

type idempotencyStore struct {
	path string
}

func newIdempotencyStore() (*idempotencyStore, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return &idempotencyStore{path: filepath.Join(paths.Home, idempotencyStateFilename)}, nil
}

func (s *idempotencyStore) replay(command, key string) ([]byte, int, bool, error) {
	command = strings.TrimSpace(command)
	key = strings.TrimSpace(key)
	if command == "" || key == "" {
		return nil, 0, false, nil
	}

	lockFile, err := lockIdempotencyFile(s.lockPath(), true)
	if err != nil {
		return nil, 0, false, err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return nil, 0, false, err
	}
	if state == nil || len(state.Entries) == 0 {
		return nil, 0, false, nil
	}

	entry, ok := state.Entries[s.entryKey(command, key)]
	if !ok {
		return nil, 0, false, nil
	}
	if entry.CreatedAt <= time.Now().Add(-idempotencyRetention).Unix() {
		return nil, 0, false, nil
	}
	if len(entry.Envelope) == 0 {
		return nil, 0, false, nil
	}
	return entry.Envelope, entry.ExitCode, true, nil
}

func (s *idempotencyStore) store(command, key string, exitCode int, envelope []byte) error {
	command = strings.TrimSpace(command)
	key = strings.TrimSpace(key)
	if command == "" || key == "" {
		return nil
	}
	if len(envelope) == 0 {
		return errors.New("idempotency envelope cannot be empty")
	}

	lockFile, err := lockIdempotencyFile(s.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return err
	}
	if state == nil {
		state = &idempotencyState{
			Version: idempotencyStateVersion,
			Entries: map[string]idempotencyEntry{},
		}
	}
	if state.Entries == nil {
		state.Entries = map[string]idempotencyEntry{}
	}

	s.prune(state)
	state.Entries[s.entryKey(command, key)] = idempotencyEntry{
		Command:   command,
		Key:       key,
		ExitCode:  exitCode,
		Envelope:  append([]byte(nil), envelope...),
		CreatedAt: time.Now().Unix(),
	}

	return s.saveState(state)
}

func (s *idempotencyStore) prune(state *idempotencyState) {
	if state == nil || len(state.Entries) == 0 {
		return
	}
	cutoff := time.Now().Add(-idempotencyRetention).Unix()
	for key, entry := range state.Entries {
		if entry.CreatedAt <= cutoff {
			delete(state.Entries, key)
		}
	}
}

func (s *idempotencyStore) entryKey(command, key string) string {
	return command + "|" + key
}

func (s *idempotencyStore) lockPath() string {
	return s.path + ".lock"
}

func (s *idempotencyStore) loadState() (*idempotencyState, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &idempotencyState{
			Version: idempotencyStateVersion,
			Entries: map[string]idempotencyEntry{},
		}, nil
	}
	if err != nil {
		return nil, err
	}

	var state idempotencyState
	if err := json.Unmarshal(data, &state); err != nil {
		// Treat malformed state as empty instead of breaking mutating flows.
		return &idempotencyState{
			Version: idempotencyStateVersion,
			Entries: map[string]idempotencyEntry{},
		}, nil
	}
	if state.Version != idempotencyStateVersion {
		return &idempotencyState{
			Version: idempotencyStateVersion,
			Entries: map[string]idempotencyEntry{},
		}, nil
	}
	if state.Entries == nil {
		state.Entries = map[string]idempotencyEntry{}
	}
	return &state, nil
}

func (s *idempotencyStore) saveState(state *idempotencyState) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil {
			slog.Debug("failed to remove temp file after rename failure", "path", tmpPath, "error", removeErr)
		}
		return err
	}
	return nil
}

func maybeReplayIdempotentResponse(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	command string,
	key string,
) (bool, int) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, 0
	}
	if !gf.JSON {
		Errorf(wErr, "--idempotency-key requires --json")
		return true, ExitUsage
	}
	store, err := newIdempotencyStore()
	if err != nil {
		ReturnError(w, "idempotency_failed", err.Error(), nil, version)
		return true, ExitInternalError
	}
	envelope, exitCode, ok, err := store.replay(command, key)
	if err != nil {
		ReturnError(w, "idempotency_failed", err.Error(), nil, version)
		return true, ExitInternalError
	}
	if !ok {
		return false, 0
	}
	_, _ = w.Write(envelope)
	return true, exitCode
}
