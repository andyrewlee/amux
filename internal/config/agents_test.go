package config

import (
	"reflect"
	"sort"
	"testing"
)

// registryNameSet is the canonical agent name set used as the single source of
// truth that every derived consumer must match exactly.
func registryNameSet(t *testing.T) map[string]struct{} {
	t.Helper()
	set := make(map[string]struct{}, len(AgentRegistry))
	for _, def := range AgentRegistry {
		if def.Name == "" {
			t.Fatal("AgentRegistry entry has empty Name")
		}
		if _, dup := set[def.Name]; dup {
			t.Fatalf("AgentRegistry has duplicate name %q", def.Name)
		}
		set[def.Name] = struct{}{}
	}
	return set
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestAgentRegistryIsCanonical is the guard that fails CI when any consumer of
// the supported-agent roster drifts from the canonical AgentRegistry. It checks,
// table-driven, that the registry name set equals exactly:
//   - the defaultAssistants() map keys,
//   - the preferredAssistantOrder entries,
//   - the set of agents IsRegisteredAgent reports as known.
//
// The theme.AgentColor non-default cases and the center assistantIsChat fallback
// set are guarded against this same registry in their own packages (importing
// config here would create an import cycle).
func TestAgentRegistryIsCanonical(t *testing.T) {
	want := registryNameSet(t)

	collect := func(names []string) map[string]struct{} {
		set := make(map[string]struct{}, len(names))
		for _, n := range names {
			set[n] = struct{}{}
		}
		return set
	}

	defaultKeys := func() []string {
		assistants := defaultAssistants()
		keys := make([]string, 0, len(assistants))
		for k := range assistants {
			keys = append(keys, k)
		}
		return keys
	}()

	registeredNames := func() []string {
		var names []string
		for _, def := range AgentRegistry {
			if IsRegisteredAgent(def.Name) {
				names = append(names, def.Name)
			}
		}
		return names
	}()

	cases := []struct {
		name string
		got  map[string]struct{}
	}{
		{"defaultAssistants keys", collect(defaultKeys)},
		{"preferredAssistantOrder", collect(preferredAssistantOrder)},
		{"IsRegisteredAgent set", collect(registeredNames)},
	}

	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, want) {
			t.Errorf("%s set %v does not equal canonical registry %v",
				tc.name, sortedKeys(tc.got), sortedKeys(want))
		}
	}

	// preferredAssistantOrder must additionally preserve registry order.
	if !reflect.DeepEqual(preferredAssistantOrder, AgentNames()) {
		t.Errorf("preferredAssistantOrder %v is not in registry order %v",
			preferredAssistantOrder, AgentNames())
	}

	// IsRegisteredAgent must reject names that are not in the registry.
	if IsRegisteredAgent("viewer") {
		t.Error("IsRegisteredAgent should reject non-registry name 'viewer'")
	}
	if IsRegisteredAgent("") {
		t.Error("IsRegisteredAgent should reject empty name")
	}

	// defaultAssistants values must mirror the registry fields exactly.
	assistants := defaultAssistants()
	for _, def := range AgentRegistry {
		got := assistants[def.Name]
		if got.Command != def.DefaultCommand ||
			got.InterruptCount != def.InterruptCount ||
			got.InterruptDelayMs != def.InterruptDelayMs {
			t.Errorf("defaultAssistants[%q] = %+v; want command=%q count=%d delay=%d",
				def.Name, got, def.DefaultCommand, def.InterruptCount, def.InterruptDelayMs)
		}
	}
}
