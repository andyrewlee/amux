package sidebar

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// sidebarRearmChainClassification is the model-internal counterpart of the
// app's rearmChainClassification: every message type TerminalModel.Update's
// type switch routes must be classified for panic re-arming. chain=true
// means a periodic chain whose arm RearmForPanic rebuilds (proven non-nil
// in terminal_update_pty_rearm_test.go); chain=false means a panic drops
// the handler's cmds, unchanged.
var sidebarRearmChainClassification = []struct {
	name  string
	msg   tea.Msg
	chain bool
}{
	{"tea.MouseClickMsg", zeroMsg[tea.MouseClickMsg](), false},
	{"tea.MouseMotionMsg", zeroMsg[tea.MouseMotionMsg](), false},
	{"tea.MouseReleaseMsg", zeroMsg[tea.MouseReleaseMsg](), false},
	{"tea.MouseWheelMsg", zeroMsg[tea.MouseWheelMsg](), false},
	{"tea.PasteMsg", zeroMsg[tea.PasteMsg](), false},
	{"tea.KeyPressMsg", zeroMsg[tea.KeyPressMsg](), false},
	// chain=true is required: stationary edge-hold auto-scroll is a
	// self-rearming chain — no pointer motion can re-fire the tick while the
	// pointer sits still, so a dropped tick ends the scroll forever unless
	// RearmForPanic emits the next one.
	{"messages.SidebarSelectionScrollTick", zeroMsg[messages.SidebarSelectionScrollTick](), true},
	{"messages.SidebarPTYOutput", zeroMsg[messages.SidebarPTYOutput](), false},
	{"messages.SidebarPTYFlush", zeroMsg[messages.SidebarPTYFlush](), true},
	{"messages.SidebarPTYStopped", zeroMsg[messages.SidebarPTYStopped](), true},
	{"messages.SidebarPTYRestart", zeroMsg[messages.SidebarPTYRestart](), true},
	{"sidebar.SidebarTabWritten", zeroMsg[SidebarTabWritten](), false},
	{"sidebar.SidebarInputFailed", zeroMsg[SidebarInputFailed](), false},
	{"sidebar.SidebarTerminalCreated", zeroMsg[SidebarTerminalCreated](), false},
	{"sidebar.SidebarTerminalReattachResult", zeroMsg[SidebarTerminalReattachResult](), false},
	{"sidebar.SidebarTerminalReattachFailed", zeroMsg[SidebarTerminalReattachFailed](), false},
	{"sidebar.SidebarTerminalCreateFailed", zeroMsg[SidebarTerminalCreateFailed](), false},
	{"messages.WorkspaceDeleted", zeroMsg[messages.WorkspaceDeleted](), false},
	{"messages.WorkspaceShelved", zeroMsg[messages.WorkspaceShelved](), false},

	// Sibling Update surfaces — TabbedSidebar.Update (tabs.go),
	// ProjectTree.Update (project_tree.go), and ChangesModel.Update
	// (model_input.go) dispatch these in addition to the tea input types
	// above. All one-shot results; no periodic chain lives in a sidebar
	// content model.
	{"messages.BranchChangesLoaded", zeroMsg[messages.BranchChangesLoaded](), false},
	{"messages.AheadBehindLoaded", zeroMsg[messages.AheadBehindLoaded](), false},
	{"sidebar.ProjectTreeDirectoryLoaded", zeroMsg[ProjectTreeDirectoryLoaded](), false},
}

// zeroMsg returns a zero-value sample of a message type — a var
// declaration, not a composite literal, so types with unexported fields
// (e.g. SidebarTabWritten) construct cleanly.
func zeroMsg[T any]() tea.Msg {
	var z T
	return z
}

// TestRearmForPanic_ClassifiesEveryModelUpdateMessage is the completeness
// contract for the model-internal panic rearm: every message type the
// sidebar's Update methods route through their type switches must appear in
// sidebarRearmChainClassification, so a new periodic chain cannot land
// silently in RearmForPanic's nil default. The dispatched set is extracted
// from the source — adding a case without classifying it fails this test
// with the type names.
func TestRearmForPanic_ClassifiesEveryModelUpdateMessage(t *testing.T) {
	dispatched := modelUpdateMsgTypeNames(t, map[string][]string{
		"terminal_update.go": {"TerminalModel"},
		"tabs.go":            {"TabbedSidebar"},
		"project_tree.go":    {"ProjectTree"},
		"model_input.go":     {"ChangesModel"},
	})

	classified := make(map[string]bool, len(sidebarRearmChainClassification))
	for _, row := range sidebarRearmChainClassification {
		classified[row.name] = row.chain
	}
	var missing []string
	for name := range dispatched {
		if _, ok := classified[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("sidebar Update message types missing a rearm classification — add each to sidebarRearmChainClassification: %v", missing)
	}

	// Classified-nil rows must produce nil: a type silently wired to a
	// starter later flips this assertion instead of silently arming.
	m := NewTerminalModel()
	for _, row := range sidebarRearmChainClassification {
		if row.chain {
			continue // non-nil proof lives in the chains' own tests
		}
		if cmd := m.RearmForPanic(row.msg); cmd != nil {
			t.Errorf("%s classified non-periodic but RearmForPanic armed a chain", row.name)
		}
	}
}

// modelUpdateMsgTypeNames extracts the type names routed by each file's
// `func (m *recvType) Update` type switches — TerminalModel plus the sibling
// content models (TabbedSidebar, ProjectTree, ChangesModel) that were the
// blind spot for non-terminal dispatch. Package-local case types are
// reported under the sidebar.* prefix.
func modelUpdateMsgTypeNames(t *testing.T, funcs map[string][]string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for file, recvTypes := range funcs {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		for _, recvType := range recvTypes {
			found := false
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Name.Name != "Update" || fd.Body == nil || !receiverIs(fd, recvType) {
					continue
				}
				found = true
				collectMsgSwitchCases(fd.Body, names)
			}
			if !found {
				t.Fatalf("func (m *%s) Update not found in %s", recvType, file)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no dispatched message types extracted — AST walker drifted from the source")
	}
	return names
}

// receiverIs reports whether the FuncDecl is a method on *want (or want).
func receiverIs(fd *ast.FuncDecl, want string) bool {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return false
	}
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == want
}

// collectMsgSwitchCases walks a function body for `switch msg := msg.(type)`
// statements and records each case's message type under its canonical name:
// package-qualified types as pkg.Type, package-local types as sidebar.Type.
func collectMsgSwitchCases(body *ast.BlockStmt, names map[string]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSwitchStmt)
		if !ok || !typeSwitchOnMsg(ts) {
			return true
		}
		for _, stmt := range ts.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				if name := caseTypeName(expr); name != "" {
					names[name] = true
				}
			}
		}
		return true
	})
}

// typeSwitchOnMsg reports whether a TypeSwitchStmt asserts on the
// identifier `msg` — the dispatch variable the top-level router uses.
func typeSwitchOnMsg(ts *ast.TypeSwitchStmt) bool {
	assign, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return false
	}
	ta, ok := assign.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return false
	}
	ident, ok := ta.X.(*ast.Ident)
	return ok && ident.Name == "msg"
}

// caseTypeName renders a case type expression to its canonical form:
// pkg.Type for selector expressions, sidebar.Type for package-local idents.
func caseTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return x.Name + "." + e.Sel.Name
		}
	case *ast.Ident:
		return "sidebar." + e.Name
	case *ast.StarExpr:
		return "*" + caseTypeName(e.X)
	}
	return ""
}
