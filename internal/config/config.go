package config

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

// DefaultConfig returns the default configuration
func DefaultConfig() (*Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Paths:         paths,
		PortStart:     6200,
		PortRangeSize: 10,
		UI:            loadUISettings(paths.ConfigPath),
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
			"droid": {
				Command:          "droid",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
			"cursor": {
				Command:          "cursor",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
		},
	}
	return cfg, nil
}
