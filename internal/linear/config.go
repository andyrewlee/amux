package linear

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config holds global Linear settings.
type Config struct {
	Accounts       []AccountConfig   `json:"accounts"`
	ActiveAccounts []string          `json:"activeAccounts"`
	WebhookSecrets map[string]string `json:"webhookSecrets"`
	Scope          ScopeConfig       `json:"scope"`
	Board          BoardConfig       `json:"board"`
	OAuth          OAuthConfig       `json:"oauth"`
}

// AccountConfig defines credentials for a Linear account.
type AccountConfig struct {
	Name string     `json:"name"`
	Auth AuthConfig `json:"auth"`
}

// AuthConfig describes the authentication mode.
type AuthConfig struct {
	Mode        string `json:"mode"` // "api_key" or "oauth"
	APIKey      string `json:"apiKey,omitempty"`
	AccessToken string `json:"accessToken,omitempty"`
}

// OAuthConfig holds OAuth application configuration.
type OAuthConfig struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	RedirectURI  string `json:"redirectUri"`
}

// ScopeConfig controls which issues show in the board.
type ScopeConfig struct {
	AssignedToMe      bool     `json:"assignedToMe"`
	IncludeProjects   []string `json:"includeProjects"`
	ExcludeProjects   []string `json:"excludeProjects"`
	IncludeTeams      []string `json:"includeTeams"`
	Labels            []string `json:"labels"`
	UpdatedWithinDays int      `json:"updatedWithinDays"`
}

// BoardConfig controls the Kanban board configuration.
type BoardConfig struct {
	Columns          []string                     `json:"columns"`
	StateMappingMode string                       `json:"-"`
	StateMapping     map[string]map[string]string `json:"stateMapping"`
	WIPLimits        map[string]int               `json:"wipLimits,omitempty"`
	ShowCanceled     bool                         `json:"showCanceled"`
}

// UnmarshalJSON supports stateMapping being "auto" or an object map.
func (b *BoardConfig) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Columns      []string        `json:"columns"`
		StateMapping json.RawMessage `json:"stateMapping"`
		WIPLimits    map[string]int  `json:"wipLimits"`
		ShowCanceled bool            `json:"showCanceled"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	b.Columns = tmp.Columns
	b.WIPLimits = tmp.WIPLimits
	b.ShowCanceled = tmp.ShowCanceled

	if len(tmp.StateMapping) == 0 {
		b.StateMappingMode = "auto"
		b.StateMapping = nil
		return nil
	}

	// Try string first.
	var mode string
	if err := json.Unmarshal(tmp.StateMapping, &mode); err == nil {
		b.StateMappingMode = mode
		b.StateMapping = nil
		return nil
	}

	// Fall back to object map.
	var mapping map[string]map[string]string
	if err := json.Unmarshal(tmp.StateMapping, &mapping); err != nil {
		return err
	}
	b.StateMappingMode = "custom"
	b.StateMapping = mapping
	return nil
}

// MarshalJSON ensures stateMapping serializes as string when mode is set.
func (b BoardConfig) MarshalJSON() ([]byte, error) {
	var stateMapping any = b.StateMapping
	if b.StateMappingMode != "" && b.StateMappingMode != "custom" {
		stateMapping = b.StateMappingMode
	}
	return json.Marshal(struct {
		Columns      []string       `json:"columns"`
		StateMapping any            `json:"stateMapping"`
		WIPLimits    map[string]int `json:"wipLimits,omitempty"`
		ShowCanceled bool           `json:"showCanceled"`
	}{
		Columns:      b.Columns,
		StateMapping: stateMapping,
		WIPLimits:    b.WIPLimits,
		ShowCanceled: b.ShowCanceled,
	})
}

// DefaultConfig returns a config with defaults.
func DefaultConfig() *Config {
	return &Config{
		Accounts:       []AccountConfig{},
		ActiveAccounts: []string{},
		WebhookSecrets: map[string]string{},
		Scope: ScopeConfig{
			AssignedToMe:      true,
			IncludeProjects:   []string{},
			ExcludeProjects:   []string{},
			IncludeTeams:      []string{},
			Labels:            []string{},
			UpdatedWithinDays: 30,
		},
		Board: BoardConfig{
			Columns:          []string{"Todo", "In Progress", "In Review", "Done"},
			StateMappingMode: "auto",
			StateMapping:     nil,
			WIPLimits:        nil,
			ShowCanceled:     false,
		},
		OAuth: OAuthConfig{},
	}
}

// LoadConfig loads Linear config from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyDefaults(cfg)
	return cfg, nil
}

// SaveConfig persists Linear config to disk.
func SaveConfig(path string, cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func applyDefaults(cfg *Config) {
	if len(cfg.Board.Columns) == 0 {
		cfg.Board.Columns = []string{"Todo", "In Progress", "In Review", "Done"}
	}
	if cfg.Board.StateMappingMode == "" {
		if len(cfg.Board.StateMapping) > 0 {
			cfg.Board.StateMappingMode = "custom"
		} else {
			cfg.Board.StateMappingMode = "auto"
		}
	}
	if cfg.Scope.UpdatedWithinDays == 0 {
		cfg.Scope.UpdatedWithinDays = 30
	}
}
