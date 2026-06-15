package theme

import (
	"reflect"
	"sort"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
)

// TestAgentColorMatchesRegistry guards that the set of agents for which
// AgentColor returns a dedicated brand color (i.e. not the ColorPrimary
// fallback) equals exactly the canonical config.AgentRegistry. A missing or
// renamed agent fails CI here.
func TestAgentColorMatchesRegistry(t *testing.T) {
	primary := ColorPrimary()

	nonDefault := map[string]struct{}{}
	for _, name := range config.AgentNames() {
		if !reflect.DeepEqual(AgentColor(name), primary) {
			nonDefault[name] = struct{}{}
		}
	}

	want := map[string]struct{}{}
	for _, name := range config.AgentNames() {
		want[name] = struct{}{}
	}

	if !reflect.DeepEqual(nonDefault, want) {
		t.Errorf("AgentColor non-default set %v does not equal registry %v",
			sortedKeys(nonDefault), sortedKeys(want))
	}

	// Unknown agents must fall back to ColorPrimary.
	if !reflect.DeepEqual(AgentColor("definitely-not-an-agent"), primary) {
		t.Error("AgentColor should fall back to ColorPrimary for unknown agents")
	}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
