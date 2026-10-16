package center

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// centerRearmChainClassification is the model-internal counterpart of the
// app's rearmChainClassification: every message type Model.Update's type
// switch routes must be classified for panic re-arming. chain=true means a
// periodic chain whose arm RearmForPanic rebuilds (proven non-nil in
// model_input_lifecycle_pty_rearm_test.go); chain=false means a panic drops
// the handler's cmds, unchanged.
//
// This table lives in package center — not alongside the app-level
// registry — because most dispatched types here are unexported and cannot
// be named cross-package, and because only an in-package test can exercise
// RearmForPanic against them.
var centerRearmChainClassification = []struct {
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
	{"messages.LaunchAgent", zeroMsg[messages.LaunchAgent](), false},
	{"messages.OpenFileInVim", zeroMsg[messages.OpenFileInVim](), false},
	{"messages.AttachRunSession", zeroMsg[messages.AttachRunSession](), false},
	{"center.ptyTabCreateResult", zeroMsg[ptyTabCreateResult](), false},
	{"center.ptyTabReattachResult", zeroMsg[ptyTabReattachResult](), false},
	{"center.ptyTabReattachFailed", zeroMsg[ptyTabReattachFailed](), false},
	{"messages.TabSessionStatus", zeroMsg[messages.TabSessionStatus](), false},
	{"messages.OpenDiff", zeroMsg[messages.OpenDiff](), false},
	{"messages.WorkspaceDeleted", zeroMsg[messages.WorkspaceDeleted](), false},
	{"messages.WorkspaceShelved", zeroMsg[messages.WorkspaceShelved](), false},
	{"center.tabSelectionResult", zeroMsg[tabSelectionResult](), false},
	{"center.selectionTickRequest", zeroMsg[selectionTickRequest](), false},
	{"center.tabDiffCmd", zeroMsg[tabDiffCmd](), false},
	{"center.tabActorRedraw", zeroMsg[tabActorRedraw](), false},
	{"center.PTYOutput", zeroMsg[PTYOutput](), false},
	{"center.PTYFlush", zeroMsg[PTYFlush](), true},
	// chain=false is deliberate: the stale-deadline rule in
	// scheduleChatCursorRefreshLocked re-arms on the next event even when a
	// panic dropped the delivered tick, so no rearm case is needed.
	{"center.PTYCursorRefresh", zeroMsg[PTYCursorRefresh](), false},
	{"center.PTYStopped", zeroMsg[PTYStopped](), true},
	{"center.PTYRestart", zeroMsg[PTYRestart](), true},
	{"center.ActivityTagFlushDue", zeroMsg[ActivityTagFlushDue](), false},
	// chain=true is required: stationary edge-hold auto-scroll is a
	// self-rearming chain — no pointer motion can re-fire the tick while the
	// pointer sits still, so a dropped tick ends the scroll forever unless
	// RearmForPanic emits the next one.
	{"center.selectionScrollTick", zeroMsg[selectionScrollTick](), true},
	{"center.diffResultMsg", zeroMsg[diffResultMsg](), false},
}

// zeroMsg returns a zero-value sample of a message type — a var
// declaration, not a composite literal, so types with unexported fields
// construct cleanly.
func zeroMsg[T any]() tea.Msg {
	var z T
	return z
}

// TestRearmForPanic_ClassifiesEveryModelUpdateMessage is the completeness
// contract for the model-internal panic rearm: every message type
// Model.Update routes through its type switch must appear in
// centerRearmChainClassification, so a new periodic chain cannot land
// silently in RearmForPanic's nil default. The dispatched set is extracted
// from the source — adding a case without classifying it fails this test
// with the type names.
func TestRearmForPanic_ClassifiesEveryModelUpdateMessage(t *testing.T) {
	dispatched := modelUpdateMsgTypeNames(t, "model_input.go", "Model")

	classified := make(map[string]bool, len(centerRearmChainClassification))
	for _, row := range centerRearmChainClassification {
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
		t.Fatalf("Model.Update message types missing a rearm classification — add each to centerRearmChainClassification: %v", missing)
	}

	// Classified-nil rows must produce nil: a type silently wired to a
	// starter later flips this assertion instead of silently arming.
	m := newTestModel()
	for _, row := range centerRearmChainClassification {
		if row.chain {
			continue // non-nil proof lives in the chains' own tests
		}
		if cmd := m.RearmForPanic(row.msg); cmd != nil {
			t.Errorf("%s classified non-periodic but RearmForPanic armed a chain", row.name)
		}
	}
}

// modelUpdateMsgTypeNames extracts the type names routed by file's
// `func (m *recvType) Update` type switch. Package-local case types are
// reported under the center.* prefix.
func modelUpdateMsgTypeNames(t *testing.T, file, recvType string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	names := map[string]bool{}
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
// package-qualified types as pkg.Type, package-local types as center.Type.
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
// pkg.Type for selector expressions, center.Type for package-local idents.
func caseTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return x.Name + "." + e.Sel.Name
		}
	case *ast.Ident:
		return "center." + e.Name
	case *ast.StarExpr:
		return "*" + caseTypeName(e.X)
	}
	return ""
}
