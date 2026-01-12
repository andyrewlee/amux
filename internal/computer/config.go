package computer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/daytona"

	"github.com/andyrewlee/amux/internal/config"
)

const (
	envAmuxDaytonaAPIKey = "AMUX_DAYTONA_API_KEY"
	envDaytonaAPIKey     = "DAYTONA_API_KEY"
	envAmuxDaytonaAPIURL = "AMUX_DAYTONA_API_URL"
	envDaytonaAPIURL     = "DAYTONA_API_URL"
	envAmuxDaytonaTarget = "AMUX_DAYTONA_TARGET"
	envDaytonaTarget     = "DAYTONA_TARGET"
	envAmuxProvider      = "AMUX_PROVIDER"

	envAmuxSpritesToken = "AMUX_SPRITES_TOKEN"
	envSpritesToken     = "SPRITES_TOKEN"
	envAmuxSpritesURL   = "AMUX_SPRITES_API_URL"
	envSpritesURL       = "SPRITES_API_URL"
)

var configKeys = []string{
	"daytonaApiKey",
	"daytonaApiUrl",
	"daytonaTarget",
	"spritesToken",
	"spritesApiUrl",
	"defaultSnapshotName",
	"snapshotAgents",
	"snapshotBaseImage",
	"settingsSync",
	"firstRunComplete",
}

// Config stores AMUX computer configuration.
// Note: Agent API keys (Anthropic, OpenAI, etc.) are NOT stored here.
// Agents authenticate via OAuth/browser login inside the sandbox.
// API keys can optionally be passed via --env flag when running agents.
type Config struct {
	DaytonaAPIKey       string             `json:"daytonaApiKey,omitempty"`
	DaytonaAPIURL       string             `json:"daytonaApiUrl,omitempty"`
	DaytonaTarget       string             `json:"daytonaTarget,omitempty"`
	SpritesToken        string             `json:"spritesToken,omitempty"`
	SpritesAPIURL       string             `json:"spritesApiUrl,omitempty"`
	DefaultSnapshotName string             `json:"defaultSnapshotName,omitempty"`
	SnapshotAgents      []string           `json:"snapshotAgents,omitempty"`
	SnapshotBaseImage   string             `json:"snapshotBaseImage,omitempty"`
	SettingsSync        SettingsSyncConfig `json:"settingsSync,omitempty"`
	FirstRunComplete    bool               `json:"firstRunComplete,omitempty"`
}

func configPath() (string, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	return paths.ConfigPath, nil
}

// LoadConfig reads AMUX computer config.
func LoadConfig() (Config, error) {
	var cfg Config
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
		return cfg, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	return cfg, nil
}

// SaveConfig writes AMUX computer config, preserving unrelated config keys (e.g. UI settings).
func SaveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	payload := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &payload)
	}
	for _, key := range configKeys {
		delete(payload, key)
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	cfgMap := map[string]any{}
	if err := json.Unmarshal(cfgBytes, &cfgMap); err != nil {
		return err
	}
	for k, v := range cfgMap {
		payload[k] = v
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ClearConfigKeys removes AMUX computer config keys from the config file.
func ClearConfigKeys() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	payload := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	for _, key := range configKeys {
		delete(payload, key)
	}
	if len(payload) == 0 {
		return os.Remove(path)
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// GetDaytonaClient returns a configured Daytona client.
func GetDaytonaClient() (*daytona.Daytona, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	apiKey := cfg.DaytonaAPIKey
	if apiKey == "" {
		apiKey = envFirst(envAmuxDaytonaAPIKey, envDaytonaAPIKey)
	}
	if apiKey == "" {
		return nil, errors.New("Daytona API key not found. Set AMUX_DAYTONA_API_KEY or run `amux auth login`.")
	}
	apiURL := cfg.DaytonaAPIURL
	if apiURL == "" {
		apiURL = envFirst(envAmuxDaytonaAPIURL, envDaytonaAPIURL)
	}
	target := cfg.DaytonaTarget
	if target == "" {
		target = envFirst(envAmuxDaytonaTarget, envDaytonaTarget)
	}
	return daytona.NewDaytona(&daytona.DaytonaConfig{
		APIKey: apiKey,
		APIURL: apiURL,
		Target: target,
	})
}

// ResolveSpritesToken returns Sprites token from config or environment without creating a client.
func ResolveSpritesToken(cfg Config) string {
	if cfg.SpritesToken != "" {
		return cfg.SpritesToken
	}
	return envFirst(envAmuxSpritesToken, envSpritesToken)
}

// ResolveSpritesAPIURL returns Sprites API URL from config or environment.
func ResolveSpritesAPIURL(cfg Config) string {
	if cfg.SpritesAPIURL != "" {
		return cfg.SpritesAPIURL
	}
	return envFirst(envAmuxSpritesURL, envSpritesURL)
}

// ResolveAPIKey returns API key from config or environment without creating a client.
func ResolveAPIKey(cfg Config) string {
	if cfg.DaytonaAPIKey != "" {
		return cfg.DaytonaAPIKey
	}
	return envFirst(envAmuxDaytonaAPIKey, envDaytonaAPIKey)
}

// ResolveSnapshotID returns snapshot ID from config or environment.
func ResolveSnapshotID(cfg Config) string {
	if cfg.DefaultSnapshotName != "" {
		return cfg.DefaultSnapshotName
	}
	return envFirst("AMUX_SNAPSHOT_ID")
}

// ResolveProviderName returns the selected provider name (override or env).
func ResolveProviderName(_ Config, override string) string {
	if override != "" {
		return strings.ToLower(strings.TrimSpace(override))
	}
	value := envFirst(envAmuxProvider)
	if value != "" {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return ""
}

// Environment variable helpers

func envFirst(keys ...string) string {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok && val != "" {
			return val
		}
	}
	return ""
}

func envIsOne(key string) bool {
	return os.Getenv(key) == "1"
}

func envDefaultTrue(keys ...string) bool {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok {
			return val != "0"
		}
	}
	return true
}
