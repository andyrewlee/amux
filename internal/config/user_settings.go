package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// UISettings stores user-facing display preferences.
type UISettings struct {
	ShowKeymapHints  bool
	Theme            string // Theme ID, defaults to "gruvbox"
	TmuxServer       string
	TmuxConfigPath   string
	TmuxSyncInterval string
}

func defaultUISettings() UISettings {
	return UISettings{
		ShowKeymapHints:  false,
		Theme:            "gruvbox",
		TmuxServer:       "",
		TmuxConfigPath:   "",
		TmuxSyncInterval: "",
	}
}

// uiSettingsRaw is the on-disk shape of the "ui" config section. Pointer
// fields distinguish "absent" from zero values.
type uiSettingsRaw struct {
	ShowKeymapHints  *bool   `json:"show_keymap_hints"`
	Theme            *string `json:"theme"`
	TmuxServer       *string `json:"tmux_server"`
	TmuxConfigPath   *string `json:"tmux_config"`
	TmuxSyncInterval *string `json:"tmux_sync_interval"`
}

// applyUISettings overlays the parsed config-file section onto the defaults.
func applyUISettings(settings UISettings, raw uiSettingsRaw) UISettings {
	if raw.ShowKeymapHints != nil {
		settings.ShowKeymapHints = *raw.ShowKeymapHints
	}
	if raw.Theme != nil {
		settings.Theme = *raw.Theme
	}
	if raw.TmuxServer != nil {
		settings.TmuxServer = *raw.TmuxServer
	}
	if raw.TmuxConfigPath != nil {
		settings.TmuxConfigPath = *raw.TmuxConfigPath
	}
	if raw.TmuxSyncInterval != nil {
		settings.TmuxSyncInterval = *raw.TmuxSyncInterval
	}
	return settings
}

func saveUISettings(path string, settings UISettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload := map[string]any{}
	if existing, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(existing, &payload)
	}

	ui, ok := payload["ui"].(map[string]any)
	if !ok || ui == nil {
		ui = map[string]any{}
	}
	ui["show_keymap_hints"] = settings.ShowKeymapHints
	ui["theme"] = settings.Theme
	ui["tmux_server"] = settings.TmuxServer
	ui["tmux_config"] = settings.TmuxConfigPath
	ui["tmux_sync_interval"] = settings.TmuxSyncInterval
	payload["ui"] = ui

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SaveUISettings persists UI settings to the config file.
func (c *Config) SaveUISettings() error {
	if c == nil || c.Paths == nil {
		return nil
	}
	return saveUISettings(c.Paths.ConfigPath, c.UI)
}

// PersistedUISettings reads UI settings from disk without mutating in-memory config state.
func (c *Config) PersistedUISettings() UISettings {
	if c == nil || c.Paths == nil {
		return defaultUISettings()
	}
	file, err := readConfigFile(c.Paths.ConfigPath)
	if err != nil {
		return defaultUISettings()
	}
	return applyUISettings(defaultUISettings(), file.UI)
}
