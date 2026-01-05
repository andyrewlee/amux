package config

import (
	"encoding/json"
	"os"
	"strings"
)

// KeyMapConfig holds user overrides for keybindings.
type KeyMapConfig struct {
	Bindings map[string][]string `json:"bindings,omitempty"`
}

// BindingFor returns the configured keys for an action, if present.
func (k KeyMapConfig) BindingFor(action string) ([]string, bool) {
	if len(k.Bindings) == 0 {
		return nil, false
	}
	if keys, ok := k.Bindings[action]; ok {
		return keys, true
	}
	if keys, ok := k.Bindings[strings.ToLower(action)]; ok {
		return keys, true
	}
	return nil, false
}

// Config holds the application configuration
type Config struct {
	Paths         *Paths
	PortStart     int
	PortRangeSize int
	Assistants    map[string]AssistantConfig
	KeyMap        KeyMapConfig
}

// AssistantConfig defines how to launch an AI assistant
type AssistantConfig struct {
	Command          string // Shell command to launch the assistant
	InterruptCount   int    // Number of Ctrl-C signals to send (default 1, claude needs 2)
	InterruptDelayMs int    // Delay between interrupts in milliseconds
}

// DefaultConfig returns the default configuration
func DefaultConfig() (*Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}

	return &Config{
		Paths:         paths,
		PortStart:     6200,
		PortRangeSize: 10,
		Assistants: map[string]AssistantConfig{
			"claude": {
				Command:          "claude",
				InterruptCount:   2,
				InterruptDelayMs: 200,
			},
			"codex": {
				Command:          "codex",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
			"gemini": {
				Command:          "gemini",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
			"amp": {
				Command:          "amp",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
			"opencode": {
				Command:          "opencode",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
		},
		KeyMap: KeyMapConfig{},
	}, nil
}

// Load loads config overrides from ~/.amux/config.json if present.
func Load() (*Config, error) {
	cfg, err := DefaultConfig()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cfg.Paths.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	var user struct {
		KeyMap KeyMapConfig `json:"keymap,omitempty"`
	}
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, err
	}

	if len(user.KeyMap.Bindings) > 0 {
		cfg.KeyMap = user.KeyMap
	}

	return cfg, nil
}
