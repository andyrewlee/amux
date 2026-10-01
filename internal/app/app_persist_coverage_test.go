package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// The persistence invariant: every tab-mutation message that carries a
// workspace identity must leave that workspace dirty (or, for the two
// messages without a WorkspaceID, the *active* workspace). A forgotten mark
// is silent data loss — the debounced flush never fires for the workspace.

// tabMutationsEnumerated lists every messages.Tab* struct that carries a
// WorkspaceID field, as found by scanning internal/messages. Keep the two
// sets in sync — a new field-carrying mutation message that is NOT in
// coveredTabMutations or ephemeralTabMessages fails the completeness test.
func tabMutationMessagesWithWorkspaceID(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	msgDir := filepath.Join("..", "messages")
	var found []string
	entries, err := filepath.Glob(filepath.Join(msgDir, "*.go"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("cannot enumerate messages dir: %v", err)
	}
	for _, file := range entries {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !strings.HasPrefix(ts.Name.Name, "Tab") {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if name.Name == "WorkspaceID" {
							found = append(found, ts.Name.Name)
						}
					}
				}
			}
		}
	}
	return found
}

// coveredTabMutations are WorkspaceID-carrying tab messages whose dispatch
// marks the workspace dirty (asserted behaviorally below).
var coveredTabMutations = map[string]bool{
	"TabDetached":         true,
	"TabReattached":       true,
	"TabStateChanged":     true,
	"TabSelectionChanged": true,
}

// ephemeralTabMessages are WorkspaceID-carrying messages whose payload is
// runtime-only state that must NOT persist — session status is rediscovered
// live, not restored from disk.
var ephemeralTabMessages = map[string]bool{
	"TabSessionStatus": true,
}

func TestTabMutationMessageCensusIsComplete(t *testing.T) {
	for _, name := range tabMutationMessagesWithWorkspaceID(t) {
		if coveredTabMutations[name] || ephemeralTabMessages[name] {
			continue
		}
		t.Errorf("messages.%s carries WorkspaceID but is in neither "+
			"coveredTabMutations nor ephemeralTabMessages — decide whether its "+
			"dispatch marks the workspace dirty and classify it", name)
	}
	for name := range coveredTabMutations {
		found := false
		for _, n := range tabMutationMessagesWithWorkspaceID(t) {
			if n == name {
				found = true
			}
		}
		if !found {
			t.Errorf("coveredTabMutations names %q which no longer exists in internal/messages", name)
		}
	}
}

// dirtyAfterMsg drives one mutating message through the tab dispatch on a
// minimal App and reports which workspace IDs were marked dirty.
func dirtyAfterMsg(a *App, msg tea.Msg) map[string]bool {
	var cmds []tea.Cmd
	a.updateTabMsg(msg, &cmds)
	return a.lifecycle.dirty
}

func TestTabMutationMessagesMarkDirty(t *testing.T) {
	ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo")
	wsID := string(ws.ID())

	cases := []struct {
		name   string
		msg    tea.Msg
		wantID string
	}{
		{"TabDetached routes by id", messages.TabDetached{WorkspaceID: wsID, Index: 0}, wsID},
		{"TabReattached", messages.TabReattached{WorkspaceID: wsID, TabID: "t1"}, wsID},
		{"TabStateChanged", messages.TabStateChanged{WorkspaceID: wsID, TabID: "t1"}, wsID},
		{"TabSelectionChanged", messages.TabSelectionChanged{WorkspaceID: wsID, ActiveIndex: 1}, wsID},
		// These two carry no WorkspaceID: they persist the *active* workspace.
		{"TabCreated persists active", messages.TabCreated{Index: 0, Name: "t"}, wsID},
		{"TabClosed persists active", messages.TabClosed{Index: 0}, wsID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{
				center:          center.New(nil),
				activeWorkspace: ws,
				lifecycle: workspaceLifecycleState{
					dirty: make(map[string]bool),
				},
			}
			dirty := dirtyAfterMsg(app, tc.msg)
			if !dirty[tc.wantID] {
				t.Fatalf("%T did not mark workspace %q dirty — the mutation "+
					"will never persist (dirty=%v)", tc.msg, tc.wantID, dirty)
			}
		})
	}
}

// The control case: a message that must NOT mark dirty. TabSessionStatus is
// routed to the center model only (runtime status, not persisted state) —
// if it ever reaches updateTabMsg and someone adds a persist call there,
// this fails and forces an explicit decision.
func TestTabSessionStatusDoesNotMarkDirty(t *testing.T) {
	app := &App{
		center: center.New(nil),
		lifecycle: workspaceLifecycleState{
			dirty: make(map[string]bool),
		},
	}
	var cmds []tea.Cmd
	handled := app.updateTabMsg(messages.TabSessionStatus{WorkspaceID: "ws-x", SessionName: "s", Status: "done"}, &cmds)
	if app.lifecycle.dirty["ws-x"] {
		t.Fatal("TabSessionStatus marked a workspace dirty — runtime status is ephemeral")
	}
	_ = handled // routing may change; the dirty invariant is what matters
}
