package github

import (
	"encoding/json"
	"errors"
	"os"
)

// Config holds GitHub integration settings.
type Config struct {
	AutoMoveToReviewOnPR bool   `json:"autoMoveToReviewOnPR"`
	AutoCompleteOnMerge  bool   `json:"autoCompleteOnMerge"`
	PreferredBaseBranch  string `json:"preferredBaseBranch"`
}

// DefaultConfig returns defaults.
func DefaultConfig() *Config {
	return &Config{AutoMoveToReviewOnPR: true, PreferredBaseBranch: "main"}
}

// LoadConfig loads config from path.
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
	if cfg.PreferredBaseBranch == "" {
		cfg.PreferredBaseBranch = "main"
	}
	return cfg, nil
}
