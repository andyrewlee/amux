package config

// AgentDef is one entry in the canonical, ordered agent registry. It is the
// single source of truth for the supported-agent roster: the preferred display
// order, the built-in default assistant config, the "is this a known chat
// agent" membership check, and the brand-color lookup are all derived from it.
// Adding, removing, or renaming an agent here updates every consumer at once.
type AgentDef struct {
	Name             string // canonical (lowercase) agent identifier
	DefaultCommand   string // shell command used to launch the agent
	InterruptCount   int    // number of Ctrl-C signals to send (claude needs 2)
	InterruptDelayMs int    // delay between interrupts in milliseconds
}

// AgentRegistry is the ordered roster of supported agents. The order here
// defines the preferred display order used throughout the UI. Keep it as the
// only place new agents are declared.
var AgentRegistry = []AgentDef{
	{Name: "claude", DefaultCommand: "claude", InterruptCount: 2, InterruptDelayMs: 200},
	{Name: "codex", DefaultCommand: "codex", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "gemini", DefaultCommand: "gemini", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "amp", DefaultCommand: "amp", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "opencode", DefaultCommand: "opencode", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "droid", DefaultCommand: "droid", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "cline", DefaultCommand: "cline", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "cursor", DefaultCommand: "agent", InterruptCount: 1, InterruptDelayMs: 0},
	{Name: "pi", DefaultCommand: "pi", InterruptCount: 1, InterruptDelayMs: 0},
}

// registeredAgentNames is the membership set derived from AgentRegistry,
// computed once for O(1) lookups.
var registeredAgentNames = func() map[string]struct{} {
	set := make(map[string]struct{}, len(AgentRegistry))
	for _, def := range AgentRegistry {
		set[def.Name] = struct{}{}
	}
	return set
}()

// AgentNames returns the canonical agent identifiers in registry (preferred
// display) order.
func AgentNames() []string {
	names := make([]string, len(AgentRegistry))
	for i, def := range AgentRegistry {
		names[i] = def.Name
	}
	return names
}

// IsRegisteredAgent reports whether name is a built-in supported agent. It
// matches the exact (already canonical/lowercase) registry name.
func IsRegisteredAgent(name string) bool {
	_, ok := registeredAgentNames[name]
	return ok
}
