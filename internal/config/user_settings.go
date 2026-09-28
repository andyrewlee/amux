package config

import (
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// UISettings stores user-facing display preferences.
type UISettings struct {
	ShowKeymapHints  bool
	Theme            string // Theme ID, defaults to "gruvbox"
	TmuxServer       string
	TmuxConfigPath   string
	TmuxSyncInterval string
	// NotifyOnDone rings a terminal bell when an agent finishes. Default off so
	// existing users are not surprised by sound.
	NotifyOnDone bool
	// ViewerCommand is the shell fragment the file viewer tab runs as
	// `<command> -- <file>`. Default "vim"; empty means vim. TUI viewers are
	// expected — a GUI command opens nothing visible in the pane.
	ViewerCommand string
}

func defaultUISettings() UISettings {
	return UISettings{
		ShowKeymapHints:  false,
		Theme:            "gruvbox",
		TmuxServer:       "",
		TmuxConfigPath:   "",
		TmuxSyncInterval: "",
		NotifyOnDone:     false,
		ViewerCommand:    "vim",
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
	NotifyOnDone     *bool   `json:"notify_on_done"`
	ViewerCommand    *string `json:"viewer_command"`
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
	if raw.NotifyOnDone != nil {
		settings.NotifyOnDone = *raw.NotifyOnDone
	}
	if raw.ViewerCommand != nil {
		settings.ViewerCommand = *raw.ViewerCommand
	}
	return settings
}

func saveUISettings(path string, settings UISettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	// Refuse to clobber an existing-but-unreadable config: the loader
	// tolerates malformed JSON (falls back to defaults), so saving over an
	// unreadable or non-object file would silently drop unrelated sections
	// the user hand-edited (e.g. "assistants").
	payload, err := readConfigForUpdate(path, readConfigPath)
	if err != nil {
		return err
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
	ui["notify_on_done"] = settings.NotifyOnDone
	ui["viewer_command"] = settings.ViewerCommand
	payload["ui"] = ui

	// Crash-safe write (temp + fsync + atomic rename) so a crash mid-save can't
	// leave a torn config.json, matching the rest of amux's persistence.
	return fsatomic.WriteJSON(path, payload)
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
	file, _ := readConfigFile(c.Paths.ConfigPath)
	return applyUISettings(defaultUISettings(), file.UI)
}
