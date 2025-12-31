package config

// Config holds the application configuration
type Config struct {
	Paths         *Paths
	PortStart     int
	PortRangeSize int
	Assistants    map[string]AssistantConfig
	Layout        LayoutConfig
}

// AssistantConfig defines how to launch an AI assistant
type AssistantConfig struct {
	Command          string // Shell command to launch the assistant
	InterruptCount   int    // Number of Ctrl-C signals to send (default 1, claude needs 2)
	InterruptDelayMs int    // Delay between interrupts in milliseconds
}

// LayoutConfig defines the three-pane layout settings
type LayoutConfig struct {
	MinChatWidth      int // Minimum width for the center pane
	MinDashboardWidth int // Minimum width for the left pane
	MinSidebarWidth   int // Minimum width for the right pane
	StartupLeftWidth  int // Initial width for dashboard
	StartupRightWidth int // Initial width for sidebar
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
			"term": {
				Command:          "bash",
				InterruptCount:   1,
				InterruptDelayMs: 0,
			},
		},
		Layout: LayoutConfig{
			MinChatWidth:      60,
			MinDashboardWidth: 20,
			MinSidebarWidth:   20,
			StartupLeftWidth:  24,
			StartupRightWidth: 72,
		},
	}, nil
}
