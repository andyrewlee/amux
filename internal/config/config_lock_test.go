//go:build !windows

package config

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// TestSaveUISettings_BlocksWhileLockHeld proves the config save transaction
// contends on the sibling flock: an externally held exclusive lock (the
// shape a second amux process takes) must park the read→mutate→write until
// released, so two processes' disjoint section saves cannot lose updates.
func TestSaveUISettings_BlocksWhileLockHeld(t *testing.T) {
	path := t.TempDir() + "/config.json"

	// Seed a section the UI save does not own; a lost-update write would
	// drop it because the save started from a pre-seed read.
	if err := saveAssistants(path, map[string]AssistantConfig{
		"claude": {Command: "claude"},
	}); err != nil {
		t.Fatalf("seed saveAssistants error = %v", err)
	}

	held, err := fsatomic.LockFile(path+".lock", false)
	if err != nil {
		t.Fatalf("LockFile error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := saveUISettings(path, UISettings{Theme: "solarized"}); err != nil {
			t.Errorf("saveUISettings error = %v", err)
		}
	}()

	select {
	case <-done:
		fsatomic.UnlockFile(held)
		t.Fatal("saveUISettings completed while the sibling lock was held externally")
	case <-time.After(200 * time.Millisecond):
	}

	fsatomic.UnlockFile(held)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("saveUISettings never completed after the sibling lock was released")
	}

	payload, err := readConfigForUpdate(path, readConfigPath)
	if err != nil {
		t.Fatalf("readConfigForUpdate error = %v", err)
	}
	ui, ok := payload["ui"].(map[string]any)
	if !ok || ui["theme"] != "solarized" {
		t.Fatalf("ui section = %v, want theme solarized", payload["ui"])
	}
	if _, ok := payload["assistants"].(map[string]any); !ok {
		t.Fatalf("assistants section lost: %v", payload)
	}
}

// TestSaveAssistants_BlocksWhileLockHeld mirrors the UI-settings lock proof
// for the assistants-section save.
func TestSaveAssistants_BlocksWhileLockHeld(t *testing.T) {
	path := t.TempDir() + "/config.json"

	held, err := fsatomic.LockFile(path+".lock", false)
	if err != nil {
		t.Fatalf("LockFile error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := saveAssistants(path, map[string]AssistantConfig{
			"codex": {Command: "codex"},
		}); err != nil {
			t.Errorf("saveAssistants error = %v", err)
		}
	}()

	select {
	case <-done:
		fsatomic.UnlockFile(held)
		t.Fatal("saveAssistants completed while the sibling lock was held externally")
	case <-time.After(200 * time.Millisecond):
	}

	fsatomic.UnlockFile(held)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("saveAssistants never completed after the sibling lock was released")
	}
}
