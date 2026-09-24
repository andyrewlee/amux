package data

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// jsonNames returns the JSON wire names a struct type produces on marshal:
// exported fields only, honoring `json:"-"` skips and the name in the tag
// when present. Embedded fields are flattened like encoding/json does.
func jsonNames(t *testing.T, typ reflect.Type) map[string]string {
	t.Helper()
	names := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := tag
		for j, r := range tag {
			if r == ',' {
				name = tag[:j]
				break
			}
		}
		if name == "" {
			name = f.Name
		}
		names[name] = f.Name
	}
	return names
}

// TestWorkspaceJSONCoversWorkspaceFields fails when Workspace gains a
// JSON-serialized field that workspaceJSON lacks — the field would persist
// on Save (fsatomic.WriteJSON marshals Workspace directly) but silently drop
// on every Load (which decodes through the hand-maintained workspaceJSON).
// The fix is to add the field to workspaceJSON or allow-list it here.
func TestWorkspaceJSONCoversWorkspaceFields(t *testing.T) {
	wsNames := jsonNames(t, reflect.TypeOf(Workspace{}))
	rawNames := jsonNames(t, reflect.TypeOf(workspaceJSON{}))

	for wire, field := range wsNames {
		if _, ok := rawNames[wire]; !ok {
			t.Errorf("Workspace.%s serializes as %q but workspaceJSON never decodes it — add the field to workspaceJSON or document the drop", field, wire)
		}
	}

	// Reverse direction: workspaceJSON fields with no Workspace counterpart
	// are legacy-read affordances — list them so the set stays reviewable.
	allowedReadOnly := map[string]string{
		// None today: every workspaceJSON field maps to a live Workspace field.
	}
	for wire, field := range rawNames {
		if _, ok := wsNames[wire]; !ok {
			if _, ok := allowedReadOnly[wire]; !ok {
				t.Errorf("workspaceJSON.%s decodes %q with no Workspace counterpart — allow-list it here if it is a deliberate legacy-read field", field, wire)
			}
		}
	}
}

// TestWorkspaceJSONRoundTripAllFields proves the decode actually lands: a
// Workspace with every serialized field populated marshals, unmarshals
// through workspaceJSON, and every workspaceJSON field comes back non-zero
// (catches tag typos the structural walk cannot).
func TestWorkspaceJSONRoundTripAllFields(t *testing.T) {
	src := Workspace{
		Name:           "ws",
		Created:        time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC),
		Branch:         "feat",
		Base:           "origin/main",
		Repo:           "/repo",
		Root:           "/repo/.amux/workspaces/ws",
		Runtime:        RuntimeLocalWorktree,
		Assistant:      "claude",
		Scripts:        ScriptsConfig{Run: "make dev"},
		ScriptMode:     "ask",
		Env:            map[string]string{"K": "V"},
		OpenTabs:       []TabInfo{{Name: "agent", Assistant: "claude"}},
		ActiveTabIndex: 1,
		Archived:       true,
		ArchivedAt:     time.Date(2026, 9, 22, 1, 2, 3, 0, time.UTC),
		Shelved:        true,
		Version:        workspaceFileVersion,
	}
	buf, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw workspaceJSON
	if err := json.Unmarshal(buf, &raw); err != nil {
		t.Fatalf("unmarshal through workspaceJSON: %v", err)
	}

	// Every workspaceJSON field must be populated — a zero field means the
	// wire key didn't decode (tag drift or a dropped field).
	rv := reflect.ValueOf(raw)
	rt := reflect.TypeOf(raw)
	for i := 0; i < rt.NumField(); i++ {
		fv := rv.Field(i)
		if fv.IsZero() {
			t.Errorf("workspaceJSON.%s came back zero after round-trip — the wire key %q did not decode", rt.Field(i).Name, rt.Field(i).Tag.Get("json"))
		}
	}
}
