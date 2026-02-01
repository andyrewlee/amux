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
	TmuxPersistence  bool
}

func defaultUISettings() UISettings {
	return UISettings{
		ShowKeymapHints:  false,
		Theme:            "gruvbox",
		TmuxServer:       "",
		TmuxConfigPath:   "",
		TmuxSyncInterval: "",
		TmuxPersistence:  true,
	}
}

func loadUISettings(path string) UISettings {
	settings := defaultUISettings()
	data, err := os.ReadFile(path)
	if err != nil {
		return settings
	}

	var raw struct {
		UI struct {
			ShowKeymapHints  *bool   `json:"show_keymap_hints"`
			Theme            *string `json:"theme"`
			TmuxServer       *string `json:"tmux_server"`
			TmuxConfigPath   *string `json:"tmux_config"`
			TmuxSyncInterval *string `json:"tmux_sync_interval"`
			TmuxPersistence  *bool   `json:"tmux_persistence"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return settings
	}
	if raw.UI.ShowKeymapHints != nil {
		settings.ShowKeymapHints = *raw.UI.ShowKeymapHints
	}
	if raw.UI.Theme != nil {
		settings.Theme = *raw.UI.Theme
	}
	if raw.UI.TmuxServer != nil {
		settings.TmuxServer = *raw.UI.TmuxServer
	}
	if raw.UI.TmuxConfigPath != nil {
		settings.TmuxConfigPath = *raw.UI.TmuxConfigPath
	}
	if raw.UI.TmuxSyncInterval != nil {
		settings.TmuxSyncInterval = *raw.UI.TmuxSyncInterval
	}
	if raw.UI.TmuxPersistence != nil {
		settings.TmuxPersistence = *raw.UI.TmuxPersistence
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
	ui["tmux_persistence"] = settings.TmuxPersistence
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
