package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// UISettings stores user-facing display preferences.
type UISettings struct {
	ShowKeymapHints    bool
	HideSidebar        bool
	HideTerminal       bool
	AutoStartAgent     bool
	SyncProfilePlugins bool
	GlobalPermissions  bool
	AutoAddPermissions bool
	DefaultAgent       string
	LastProfile        string // Most recently selected profile name
	LastAllowEdits     bool   // Last state of "allow edits" checkbox for new workspaces
	LastIsolated       bool   // Last state of "run isolated" checkbox for new workspaces
	LastSkipPermissions bool  // Last state of "skip permissions" checkbox for new workspaces
	Theme              string // Theme ID, defaults to "gruvbox"
	TmuxServer         string
	TmuxConfigPath     string
	TmuxSyncInterval   string
	TmuxPersistence    bool
	BellOnReady        bool   // Ring terminal bell when agent finishes
	IDE                string // CLI command for IDE (e.g., "code", "cursor", "pycharm")
}

func defaultUISettings() UISettings {
	return UISettings{
		ShowKeymapHints:    false,
		HideTerminal:       true,
		AutoStartAgent:     true,
		SyncProfilePlugins: true,
		GlobalPermissions:  true,
		AutoAddPermissions: false,
		Theme:              "gruvbox",
		TmuxServer:         "",
		TmuxConfigPath:     "",
		TmuxSyncInterval:   "",
		TmuxPersistence:    true,
		BellOnReady:        true,
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
			ShowKeymapHints    *bool   `json:"show_keymap_hints"`
			HideSidebar        *bool   `json:"hide_sidebar"`
			HideTerminal       *bool   `json:"hide_terminal"`
			AutoStartAgent     *bool   `json:"auto_start_agent"`
			SyncProfilePlugins *bool   `json:"sync_profile_plugins"`
			GlobalPermissions  *bool   `json:"global_permissions"`
			AutoAddPermissions *bool   `json:"auto_add_permissions"`
			DefaultAgent       *string `json:"default_agent"`
			LastProfile        *string `json:"last_profile"`
			LastAllowEdits      *bool   `json:"last_allow_edits"`
			LastIsolated        *bool   `json:"last_isolated"`
			LastSkipPermissions *bool   `json:"last_skip_permissions"`
			Theme              *string `json:"theme"`
			TmuxServer         *string `json:"tmux_server"`
			TmuxConfigPath     *string `json:"tmux_config"`
			TmuxSyncInterval   *string `json:"tmux_sync_interval"`
			TmuxPersistence    *bool   `json:"tmux_persistence"`
			BellOnReady        *bool   `json:"bell_on_ready"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return settings
	}
	if raw.UI.ShowKeymapHints != nil {
		settings.ShowKeymapHints = *raw.UI.ShowKeymapHints
	}
	if raw.UI.HideSidebar != nil {
		settings.HideSidebar = *raw.UI.HideSidebar
	}
	if raw.UI.HideTerminal != nil {
		settings.HideTerminal = *raw.UI.HideTerminal
	}
	if raw.UI.AutoStartAgent != nil {
		settings.AutoStartAgent = *raw.UI.AutoStartAgent
	}
	if raw.UI.SyncProfilePlugins != nil {
		settings.SyncProfilePlugins = *raw.UI.SyncProfilePlugins
	}
	if raw.UI.GlobalPermissions != nil {
		settings.GlobalPermissions = *raw.UI.GlobalPermissions
	}
	if raw.UI.AutoAddPermissions != nil {
		settings.AutoAddPermissions = *raw.UI.AutoAddPermissions
	}
	if raw.UI.DefaultAgent != nil {
		settings.DefaultAgent = *raw.UI.DefaultAgent
	}
	if raw.UI.LastProfile != nil {
		settings.LastProfile = *raw.UI.LastProfile
	}
	if raw.UI.LastAllowEdits != nil {
		settings.LastAllowEdits = *raw.UI.LastAllowEdits
	}
	if raw.UI.LastIsolated != nil {
		settings.LastIsolated = *raw.UI.LastIsolated
	}
	if raw.UI.LastSkipPermissions != nil {
		settings.LastSkipPermissions = *raw.UI.LastSkipPermissions
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
	if raw.UI.BellOnReady != nil {
		settings.BellOnReady = *raw.UI.BellOnReady
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
	ui["hide_sidebar"] = settings.HideSidebar
	ui["hide_terminal"] = settings.HideTerminal
	ui["auto_start_agent"] = settings.AutoStartAgent
	ui["sync_profile_plugins"] = settings.SyncProfilePlugins
	ui["global_permissions"] = settings.GlobalPermissions
	ui["auto_add_permissions"] = settings.AutoAddPermissions
	ui["default_agent"] = settings.DefaultAgent
	ui["last_profile"] = settings.LastProfile
	ui["last_allow_edits"] = settings.LastAllowEdits
	ui["last_isolated"] = settings.LastIsolated
	ui["last_skip_permissions"] = settings.LastSkipPermissions
	ui["theme"] = settings.Theme
	ui["tmux_server"] = settings.TmuxServer
	ui["tmux_config"] = settings.TmuxConfigPath
	ui["tmux_sync_interval"] = settings.TmuxSyncInterval
	ui["tmux_persistence"] = settings.TmuxPersistence
	ui["bell_on_ready"] = settings.BellOnReady
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
