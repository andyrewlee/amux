package config

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/andyrewlee/amux/internal/logging"

	"github.com/andyrewlee/amux/internal/validation"
)

// Config holds the application configuration
type Config struct {
	Paths         *Paths
	PortStart     int
	PortRangeSize int
	Assistants    map[string]AssistantConfig
	UI            UISettings
}

// AssistantConfig defines how to launch an AI assistant
type AssistantConfig struct {
	Command          string // Shell command to launch the assistant
	InterruptCount   int    // Number of Ctrl-C signals to send (default 1, claude needs 2)
	InterruptDelayMs int    // Delay between interrupts in milliseconds
}

type assistantConfigRaw struct {
	Command          string `json:"command"`
	InterruptCount   *int   `json:"interrupt_count"`
	InterruptDelayMs *int   `json:"interrupt_delay_ms"`
}

const fallbackDefaultAssistant = "claude"

var preferredAssistantOrder = []string{
	"claude",
	"codex",
	"gemini",
	"amp",
	"opencode",
	"droid",
	"cline",
	"cursor",
	"pi",
}

// DefaultConfig returns the default configuration
func DefaultConfig() (*Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}

	// The config file is parsed exactly once; each section loader receives
	// its slice of the parsed structure. A corrupted file therefore surfaces
	// one warning here instead of two independent silent default fallbacks.
	file, err := readConfigFile(paths.ConfigPath)
	if err != nil {
		logging.Warn("config: failed to parse %s; using defaults: %v", paths.ConfigPath, err)
	}

	assistants := defaultAssistants()
	applyAssistantOverrides(assistants, file.Assistants)

	cfg := &Config{
		Paths:         paths,
		PortStart:     6200,
		PortRangeSize: 10,
		UI:            applyUISettings(defaultUISettings(), file.UI),
		Assistants:    assistants,
	}
	return cfg, nil
}

// configFile is the single on-disk config schema.
type configFile struct {
	Assistants map[string]assistantConfigRaw `json:"assistants"`
	UI         uiSettingsRaw                 `json:"ui"`
}

// readConfigFile parses the config file once. A missing file is not an
// error; a corrupted file returns the parse error along with zero contents.
func readConfigFile(path string) (configFile, error) {
	var file configFile
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return file, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return configFile{}, err
	}
	return file, nil
}

// AssistantNames returns assistant IDs in deterministic display order.
func (c *Config) AssistantNames() []string {
	if c == nil {
		return nil
	}
	return orderedAssistantNames(c.Assistants)
}

// IsAssistantKnown reports whether assistant exists in loaded config.
func (c *Config) IsAssistantKnown(assistant string) bool {
	if c == nil || len(c.Assistants) == 0 {
		return false
	}
	_, ok := c.Assistants[normalizeAssistantName(assistant)]
	return ok
}

// ResolvedDefaultAssistant returns a valid default assistant name.
func (c *Config) ResolvedDefaultAssistant() string {
	if c == nil {
		return fallbackDefaultAssistant
	}
	return canonicalDefaultAssistant(fallbackDefaultAssistant, c.Assistants)
}

func defaultAssistants() map[string]AssistantConfig {
	return map[string]AssistantConfig{
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
		"droid": {
			Command:          "droid",
			InterruptCount:   1,
			InterruptDelayMs: 0,
		},
		"cline": {
			Command:          "cline",
			InterruptCount:   1,
			InterruptDelayMs: 0,
		},
		"cursor": {
			Command:          "agent",
			InterruptCount:   1,
			InterruptDelayMs: 0,
		},
		"pi": {
			Command:          "pi",
			InterruptCount:   1,
			InterruptDelayMs: 0,
		},
	}
}

// applyAssistantOverrides overlays parsed config-file assistant entries onto
// the built-in defaults.
func applyAssistantOverrides(assistants map[string]AssistantConfig, overrides map[string]assistantConfigRaw) {
	for name, override := range overrides {
		normalized := normalizeAssistantName(name)
		if normalized == "" {
			continue
		}
		if err := validation.ValidateAssistant(normalized); err != nil {
			continue
		}

		cfg := assistants[normalized]
		if cmd := strings.TrimSpace(override.Command); cmd != "" {
			cfg.Command = cmd
		}
		if override.InterruptCount != nil {
			cfg.InterruptCount = *override.InterruptCount
		}
		if override.InterruptDelayMs != nil {
			cfg.InterruptDelayMs = *override.InterruptDelayMs
		}

		if cfg.Command == "" {
			continue
		}
		if cfg.InterruptCount <= 0 {
			cfg.InterruptCount = 1
		}
		if cfg.InterruptDelayMs < 0 {
			cfg.InterruptDelayMs = 0
		}

		assistants[normalized] = cfg
	}
}

func orderedAssistantNames(assistants map[string]AssistantConfig) []string {
	if len(assistants) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(assistants))
	names := make([]string, 0, len(assistants))

	for _, name := range preferredAssistantOrder {
		if _, ok := assistants[name]; ok {
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}

	var extras []string
	for name := range assistants {
		if _, ok := seen[name]; ok {
			continue
		}
		extras = append(extras, name)
	}
	sort.Strings(extras)
	names = append(names, extras...)

	return names
}

func canonicalDefaultAssistant(candidate string, assistants map[string]AssistantConfig) string {
	name := normalizeAssistantName(candidate)
	if name != "" {
		if _, ok := assistants[name]; ok {
			return name
		}
	}
	if _, ok := assistants[fallbackDefaultAssistant]; ok {
		return fallbackDefaultAssistant
	}
	names := orderedAssistantNames(assistants)
	if len(names) > 0 {
		return names[0]
	}
	return fallbackDefaultAssistant
}

func normalizeAssistantName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
