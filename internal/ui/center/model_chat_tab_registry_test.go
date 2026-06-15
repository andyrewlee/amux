package center

import (
	"testing"
)

// TestAssistantIsChatFallbackRejectsUnknown smoke-exercises the nil-config
// fallback branch of assistantIsChat: with a nil config the method delegates to
// config.IsRegisteredAgent, so an unregistered name must take the false branch.
// This is not a roster-drift guard — that lives in config's
// TestAgentRegistryIsCanonical and theme's TestAgentColorMatchesRegistry.
// Because the fallback delegates to IsRegisteredAgent, asserting the positive
// set equals AgentNames() would be definitionally true, so we only exercise the
// fallback's false branch here.
func TestAssistantIsChatFallbackRejectsUnknown(t *testing.T) {
	m := &Model{} // nil config forces the registry fallback branch

	// Unknown agents must not be treated as chat agents in the fallback.
	if m.assistantIsChat("definitely-not-an-agent") {
		t.Error("assistantIsChat fallback should reject unknown agents")
	}
}
