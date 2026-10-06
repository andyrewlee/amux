package sidebar

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// fakeDirEntry is a minimal os.DirEntry for injected readDir fixtures.
type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string { return f.name }
func (f fakeDirEntry) IsDir() bool  { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode {
	if f.dir {
		return fs.ModeDir
	}
	return 0
}
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("fake entry") }

// gatedReadDir is a deterministic readDir seam: every call is recorded, and a
// call whose path has an entry in gates blocks until that channel is closed,
// so tests can hold a directory read open while they inspect pending state
// and can order completions precisely without sleeps.
type gatedReadDir struct {
	mu             sync.Mutex
	calls          []string
	inflight       int
	maxInflight    int
	inflightByPath map[string]int
	gates          map[string]chan struct{}
	listing        map[string][]os.DirEntry
	errs           map[string]error
}

func (g *gatedReadDir) read(path string) ([]os.DirEntry, error) {
	g.mu.Lock()
	g.calls = append(g.calls, path)
	g.inflight++
	if g.inflightByPath == nil {
		g.inflightByPath = map[string]int{}
	}
	g.inflightByPath[path]++
	if g.inflight > g.maxInflight {
		g.maxInflight = g.inflight
	}
	gate := g.gates[path]
	g.mu.Unlock()

	if gate != nil {
		<-gate
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.inflight--
	g.inflightByPath[path]--
	if g.inflightByPath[path] == 0 {
		delete(g.inflightByPath, path)
	}
	return g.listing[path], g.errs[path]
}

// inflightCount reports how many readDir calls are blocked right now.
func (g *gatedReadDir) inflightCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight
}

// inflightPaths reports which readDir calls are still in flight, for failure
// diagnostics. Path order is nondeterministic.
func (g *gatedReadDir) inflightPaths() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var paths []string
	for p := range g.inflightByPath {
		paths = append(paths, filepath.Base(p))
	}
	return paths
}

func (g *gatedReadDir) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

func (g *gatedReadDir) called(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.calls {
		if c == path {
			return true
		}
	}
	return false
}

func (g *gatedReadDir) release(path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ch, ok := g.gates[path]; ok && ch != nil {
		close(ch)
		g.gates[path] = nil
	}
}

// runCmdAsync executes a command on a goroutine and yields whatever it
// produces: for a single dispatched job the SafeBatch collapses to the leaf,
// so the channel yields the result message itself; for multiple jobs it
// yields a BatchMsg of not-yet-run leaves.
func runCmdAsync(cmd tea.Cmd) <-chan tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	return ch
}

// deliver feeds one message through Update and synchronously drains any
// follow-up commands the model produced (they must not hit gated paths).
func deliver(t *testing.T, m *ProjectTree, msg tea.Msg) {
	t.Helper()
	_, follow := m.Update(msg)
	pumpTree(t, m, follow)
}

// drainAsync runs cmd on a goroutine, delivers its message(s) — recursing
// into BatchMsg leaves — and pumps follow-ups. Safe only when no gated read
// stays closed.
func drainAsync(t *testing.T, m *ProjectTree, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := <-runCmdAsync(cmd)
	if batch, ok := msg.(tea.BatchMsg); ok {
		chans := make([]<-chan tea.Msg, 0, len(batch))
		for _, leaf := range batch {
			chans = append(chans, runCmdAsync(leaf))
		}
		for _, ch := range chans {
			deliver(t, m, <-ch)
		}
		return
	}
	deliver(t, m, msg)
}

// wsFor builds a workspace rooted at path.
func wsFor(path string) *data.Workspace {
	return data.NewWorkspace("ws", "ws", "main", filepath.Dir(path), path)
}

// gatedTree builds a tree whose readDir is the fixture.
func gatedTree(g *gatedReadDir) *ProjectTree {
	m := NewProjectTree()
	m.readDir = g.read
	return m
}

func TestProjectTreeAsyncDelayedRootLoad(t *testing.T) {
	root := t.TempDir()
	g := &gatedReadDir{
		gates:   map[string]chan struct{}{root: make(chan struct{})},
		listing: map[string][]os.DirEntry{root: {fakeDirEntry{"a.txt", false}}},
	}
	m := gatedTree(g)
	m.SetSize(40, 10)
	m.SetShowKeymapHints(false)

	res := runCmdAsync(m.SetWorkspace(wsFor(root)))

	// While the read is held open the tree shows the loading state and no rows.
	select {
	case <-res:
		t.Fatal("root read returned while its gate was closed")
	default:
	}
	if len(m.flatNodes) != 0 {
		t.Fatalf("expected no rows while root load is pending, got %d", len(m.flatNodes))
	}
	if got := m.View(); !strings.Contains(got, "Loading") {
		t.Fatalf("expected loading indicator, got %q", got)
	}

	g.release(root)
	deliver(t, m, <-res)
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "a.txt" {
		t.Fatalf("expected [a.txt] after root load, got %+v", m.flatNodes)
	}
}

// gatedSeededTree builds the seeded two-dir fixture through the gated seam:
// root/ contains alpha/ (dir, holds nested.txt), beta/ (dir), one.txt,
// two.txt — matching newSeededProjectTree's shape.
func gatedSeededTree(t *testing.T, g *gatedReadDir) (*ProjectTree, string) {
	t.Helper()
	root := t.TempDir()
	if g.listing == nil {
		g.listing = map[string][]os.DirEntry{}
	}
	g.listing[root] = []os.DirEntry{
		fakeDirEntry{"alpha", true},
		fakeDirEntry{"beta", true},
		fakeDirEntry{"one.txt", false},
		fakeDirEntry{"two.txt", false},
	}
	g.listing[filepath.Join(root, "alpha")] = []os.DirEntry{
		fakeDirEntry{"nested.txt", false},
	}
	m := gatedTree(g)
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))
	if len(m.flatNodes) != 4 {
		t.Fatalf("expected 4 seeded nodes, got %d", len(m.flatNodes))
	}
	return m, root
}

func TestProjectTreeAsyncDelayedChildLoad(t *testing.T) {
	g := &gatedReadDir{gates: map[string]chan struct{}{}}
	m, root := gatedSeededTree(t, g)

	alphaPath := filepath.Join(root, "alpha")
	g.gates[alphaPath] = make(chan struct{})

	res := runCmdAsync(m.expandNode(m.flatNodes[0]))
	select {
	case <-res:
		t.Fatal("child read returned while its gate was closed")
	default:
	}
	if !m.flatNodes[0].Expanded || m.flatNodes[0].pendingRequest == 0 {
		t.Fatalf("expected alpha expanded with pending marker, got %+v", m.flatNodes[0])
	}
	if !strings.Contains(m.View(), "…") {
		t.Fatal("expected loading marker on pending directory row")
	}

	g.release(alphaPath)
	deliver(t, m, <-res)
	if len(m.flatNodes) != 5 || m.flatNodes[1].Name != "nested.txt" {
		t.Fatalf("expected nested.txt under alpha after load, got %+v", m.flatNodes)
	}
}

func TestProjectTreeAsyncOldWorkspaceResultDiscarded(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	g := &gatedReadDir{
		gates: map[string]chan struct{}{rootA: make(chan struct{})},
		listing: map[string][]os.DirEntry{
			rootA: {fakeDirEntry{"fromA.txt", false}},
			rootB: {fakeDirEntry{"fromB.txt", false}},
		},
	}
	m := gatedTree(g)

	stale := runCmdAsync(m.SetWorkspace(wsFor(rootA)))
	pumpTree(t, m, m.SetWorkspace(wsFor(rootB)))
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "fromB.txt" {
		t.Fatalf("expected workspace B's listing, got %+v", m.flatNodes)
	}

	g.release(rootA)
	deliver(t, m, <-stale)
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "fromB.txt" {
		t.Fatalf("stale workspace A result mutated the tree: %+v", m.flatNodes)
	}
}

func TestProjectTreeAsyncRepeatedRefreshKeepsNewest(t *testing.T) {
	root := t.TempDir()
	g := &gatedReadDir{listing: map[string][]os.DirEntry{
		root: {fakeDirEntry{"a.txt", false}},
	}}
	m := gatedTree(g)
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))

	// First refresh completes its read but the result is held; a second
	// refresh supersedes it before delivery.
	stale := m.reloadTree()()
	cmd2 := m.reloadTree()
	m.Update(stale)
	if m.flatNodes[0].Name != "a.txt" {
		t.Fatalf("stale refresh result changed the tree: %+v", m.flatNodes)
	}
	g.listing[root] = []os.DirEntry{fakeDirEntry{"b.txt", false}}
	pumpTree(t, m, cmd2)
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "b.txt" {
		t.Fatalf("expected newest refresh result, got %+v", m.flatNodes)
	}
}

func TestProjectTreeAsyncCollapseDuringPendingDiscardsResult(t *testing.T) {
	g := &gatedReadDir{gates: map[string]chan struct{}{}}
	m, root := gatedSeededTree(t, g)
	alphaPath := filepath.Join(root, "alpha")
	g.gates[alphaPath] = make(chan struct{})

	alpha := m.flatNodes[0]
	res := runCmdAsync(m.expandNode(alpha))
	m.collapseNode(alpha)

	g.release(alphaPath)
	deliver(t, m, <-res)
	if alpha.Expanded || len(alpha.Children) != 0 {
		t.Fatalf("collapsed node's late result installed children: %+v", alpha)
	}
	if len(m.flatNodes) != 4 {
		t.Fatalf("flat list changed after stale collapse result: %d", len(m.flatNodes))
	}
}

func TestProjectTreeAsyncReExpandAfterCollapseLoads(t *testing.T) {
	g := &gatedReadDir{gates: map[string]chan struct{}{}}
	m, root := gatedSeededTree(t, g)
	alphaPath := filepath.Join(root, "alpha")
	g.gates[alphaPath] = make(chan struct{})

	alpha := m.flatNodes[0]
	stale := runCmdAsync(m.expandNode(alpha))
	m.collapseNode(alpha)

	// Re-expand issues a fresh request under the same path; once the gate
	// opens, both the stale and current reads complete.
	g.release(alphaPath)
	pumpTree(t, m, m.expandNode(alpha))
	if len(m.flatNodes) != 5 || m.flatNodes[1].Name != "nested.txt" {
		t.Fatalf("expected nested.txt after re-expand, got %+v", m.flatNodes)
	}

	// The superseded read's result arrives last and must not corrupt state.
	deliver(t, m, <-stale)
	if len(m.flatNodes) != 5 || m.flatNodes[1].Name != "nested.txt" {
		t.Fatalf("stale result corrupted re-expanded tree: %+v", m.flatNodes)
	}
}

func TestProjectTreeAsyncNestedExpansionRestored(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	sub := filepath.Join(alpha, "sub")
	g := &gatedReadDir{listing: map[string][]os.DirEntry{
		root:  {fakeDirEntry{"alpha", true}, fakeDirEntry{"one.txt", false}},
		alpha: {fakeDirEntry{"sub", true}},
		sub:   {fakeDirEntry{"deep.txt", false}},
	}}
	m := gatedTree(g)
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))

	// Expand alpha then its sub directory; refresh must restore both levels.
	pumpTree(t, m, m.expandNode(m.flatNodes[0])) // alpha
	pumpTree(t, m, m.expandNode(m.flatNodes[1])) // sub
	if len(m.flatNodes) != 4 {
		t.Fatalf("expected [alpha sub deep.txt one.txt], got %+v", m.flatNodes)
	}

	pumpTree(t, m, m.reloadTree())

	if len(m.flatNodes) != 4 {
		t.Fatalf("expected nested expansion restored after refresh, got %+v", m.flatNodes)
	}
	if !m.flatNodes[0].Expanded || m.flatNodes[0].Name != "alpha" {
		t.Fatalf("alpha lost expansion: %+v", m.flatNodes[0])
	}
	if m.flatNodes[2].Name != "deep.txt" {
		t.Fatalf("expected deep.txt at index 2, got %+v", m.flatNodes[2])
	}
}

func TestProjectTreeAsyncUserNavigationOverridesSelectionRestore(t *testing.T) {
	g := &gatedReadDir{gates: map[string]chan struct{}{}}
	m, root := gatedSeededTree(t, g)
	m.Focus()

	// Cursor on two.txt, then refresh with a held root read.
	m.cursor = 3
	if m.flatNodes[3].Name != "two.txt" {
		t.Fatalf("expected two.txt at index 3, got %+v", m.flatNodes[3])
	}
	g.gates[root] = make(chan struct{})
	cmd := m.reloadTree()

	// The user navigates while the refresh is in flight.
	m.moveCursor(-2)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	g.release(root)
	drainAsync(t, m, cmd)

	// The selection restore must not yank the cursor back to two.txt.
	if m.flatNodes[m.cursor].Name != "beta" {
		t.Fatalf("refresh yanked selection over user navigation: cursor=%d (%q)",
			m.cursor, m.flatNodes[m.cursor].Name)
	}
}

func TestProjectTreeAsyncDeletedSelectionTargetClamps(t *testing.T) {
	g := &gatedReadDir{}
	m, root := gatedSeededTree(t, g)
	m.Focus()

	m.cursor = 3 // two.txt
	g.listing[root] = []os.DirEntry{
		fakeDirEntry{"alpha", true},
		fakeDirEntry{"beta", true},
	} // two.txt deleted between refreshes

	pumpTree(t, m, m.reloadTree())

	if m.pendingSelectedPath != "" {
		t.Fatal("pending selection never resolved for a deleted target")
	}
	if m.cursor < 0 || m.cursor >= len(m.flatNodes) {
		t.Fatalf("cursor not clamped to a valid row: %d of %d", m.cursor, len(m.flatNodes))
	}
}

func TestProjectTreeAsyncReadDirErrorMarkerAndRetry(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	g := &gatedReadDir{
		listing: map[string][]os.DirEntry{
			root: {fakeDirEntry{"alpha", true}},
		},
		errs: map[string]error{alpha: errors.New("perm denied")},
	}
	m := gatedTree(g)
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))
	m.SetSize(60, 10)
	m.SetShowKeymapHints(false)

	pumpTree(t, m, m.expandNode(m.flatNodes[0]))

	node := m.flatNodes[0]
	if node.loadErr == "" {
		t.Fatal("expected sanitized load error marker on the node")
	}
	if node.pendingRequest != 0 {
		t.Fatal("error result left the request marked pending")
	}
	if len(node.Children) != 0 {
		t.Fatalf("error result masqueraded as an empty directory: %+v", node.Children)
	}
	if !strings.Contains(m.View(), "perm denied") {
		t.Fatalf("expected error hint in view, got %q", m.View())
	}

	// Retry: collapse, fix the backend, re-expand.
	m.collapseNode(node)
	delete(g.errs, alpha)
	g.listing[alpha] = []os.DirEntry{fakeDirEntry{"recovered.txt", false}}
	pumpTree(t, m, m.expandNode(node))
	if len(node.Children) != 1 || node.Children[0].Name != "recovered.txt" {
		t.Fatalf("retry did not load children: %+v", node.Children)
	}
	if node.loadErr != "" {
		t.Fatalf("stale error marker survived successful retry: %q", node.loadErr)
	}
}
