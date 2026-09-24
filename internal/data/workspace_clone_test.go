package data

import (
	"reflect"
	"testing"
)

// TestWorkspaceClone_DeepCopiesReferenceFields mutates every reference field
// on the source after cloning and asserts the clone is unaffected — the
// async-snapshot contract.
func TestWorkspaceClone_DeepCopiesReferenceFields(t *testing.T) {
	src := &Workspace{
		Name:     "ws",
		Repo:     "/repo",
		Root:     "/repo/ws",
		Env:      map[string]string{"A": "1"},
		OpenTabs: []TabInfo{{Name: "tab-1", Assistant: "claude"}},
	}
	clone := src.Clone()

	src.Env["A"] = "mutated"
	src.Env["B"] = "added"
	src.OpenTabs[0].Name = "mutated"
	src.OpenTabs = append(src.OpenTabs, TabInfo{Name: "tab-2"})

	if clone.Env["A"] != "1" || len(clone.Env) != 1 {
		t.Fatalf("clone.Env aliased source: %v", clone.Env)
	}
	if clone.OpenTabs[0].Name != "tab-1" || len(clone.OpenTabs) != 1 {
		t.Fatalf("clone.OpenTabs aliased source: %v", clone.OpenTabs)
	}
}

// TestWorkspaceClone_CoversAllReferenceFields walks Workspace's fields via
// reflection and fails on any map/slice/pointer/func/chan field outside the
// known-copied set — a new reference field added without a Clone branch is a
// silent aliasing bug this test catches at add time.
func TestWorkspaceClone_CoversAllReferenceFields(t *testing.T) {
	copied := map[string]bool{"Env": true, "OpenTabs": true}
	wsType := reflect.TypeOf(Workspace{})
	for i := 0; i < wsType.NumField(); i++ {
		f := wsType.Field(i)
		switch f.Type.Kind() {
		case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Func, reflect.Chan:
			if !copied[f.Name] {
				t.Errorf("Workspace.%s (%s) is a reference type not deep-copied by Clone — add a copy branch and extend this test's set", f.Name, f.Type)
			}
		}
	}
}

// TestWorkspaceClone_ValueFieldsVerbatim guards identity fields: a clone is
// the same record, so storeID and the scalar identity fields copy verbatim.
func TestWorkspaceClone_ValueFieldsVerbatim(t *testing.T) {
	src := &Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws", Assistant: "claude", Shelved: true}
	src.storeID = "deadbeefcafebabe"
	clone := src.Clone()
	if clone.ID() != src.ID() || clone.Name != src.Name || clone.Shelved != src.Shelved {
		t.Fatalf("clone lost value fields: %+v vs %+v", clone, src)
	}
}
