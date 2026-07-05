package pty

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/config"
)

func TestAgentManager_SendInterrupt(t *testing.T) {
	m := NewAgentManager(testConfig())

	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}
	defer term.Close()

	agent := &Agent{
		Type:     AgentType("codex"),
		Terminal: term,
		Config: config.AssistantConfig{
			InterruptCount: 1,
		},
	}

	err = m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt failed: %v", err)
	}
}

func TestAgentManager_SendInterrupt_MultipleWithDelay(t *testing.T) {
	m := NewAgentManager(testConfig())

	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}
	defer term.Close()

	agent := &Agent{
		Type:     AgentType("claude"),
		Terminal: term,
		Config: config.AssistantConfig{
			InterruptCount:   3,
			InterruptDelayMs: 10,
		},
	}

	err = m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt with multiple interrupts failed: %v", err)
	}
}

func TestAgentManager_SendInterrupt_NilTerminal(t *testing.T) {
	m := NewAgentManager(testConfig())

	agent := &Agent{
		Type:     AgentType("claude"),
		Terminal: nil,
		Config: config.AssistantConfig{
			InterruptCount: 2,
		},
	}

	// Should return nil when terminal is nil
	err := m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt with nil terminal should return nil, got %v", err)
	}
}

func TestAgentManager_SendInterrupt_ZeroCount(t *testing.T) {
	m := NewAgentManager(testConfig())

	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}
	defer term.Close()

	agent := &Agent{
		Type:     AgentType("claude"),
		Terminal: term,
		Config: config.AssistantConfig{
			InterruptCount: 0, // unconfigured: floors to a single interrupt
		},
	}

	// A zero/unset InterruptCount must still deliver one Ctrl-C so a user's
	// interrupt is never silently swallowed (e.g. for viewer agents).
	err = m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt with zero count should succeed, got %v", err)
	}
}

func TestInterruptSettings(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.AssistantConfig
		wantCount int
		wantDelay time.Duration
	}{
		{
			name: "keeps normal values",
			cfg: config.AssistantConfig{
				InterruptCount:   3,
				InterruptDelayMs: 10,
			},
			wantCount: 3,
			wantDelay: 10 * time.Millisecond,
		},
		{
			name: "floors zero count and negative delay",
			cfg: config.AssistantConfig{
				InterruptCount:   0,
				InterruptDelayMs: -1,
			},
			wantCount: 1,
			wantDelay: 0,
		},
		{
			name: "caps oversized values before use",
			cfg: config.AssistantConfig{
				InterruptCount:   maxInterruptCount + 100,
				InterruptDelayMs: int(maxInterruptDelay/time.Millisecond) + 1000,
			},
			wantCount: maxInterruptCount,
			wantDelay: maxInterruptDelay,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCount, gotDelay := interruptSettings(tt.cfg)
			if gotCount != tt.wantCount || gotDelay != tt.wantDelay {
				t.Fatalf("interruptSettings() = (%d, %s), want (%d, %s)",
					gotCount, gotDelay, tt.wantCount, tt.wantDelay)
			}
		})
	}
}
