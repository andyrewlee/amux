package sidebar

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestProjectTreeAsyncBoundedConcurrency(t *testing.T) {
	root := t.TempDir()
	g := &gatedReadDir{gates: map[string]chan struct{}{}}
	g.listing = map[string][]os.DirEntry{root: {}}
	m := NewProjectTree()
	m.readDir = g.read
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))
	baseline := g.callCount() // the seeding root read

	// Enqueue 10 directory reads — only the cap may dispatch.
	var dirs []*projectTreeNode
	for i := 0; i < 10; i++ {
		p := filepath.Join(root, "d"+string(rune('a'+i)))
		dirs = append(dirs, &projectTreeNode{
			Name: "d" + string(rune('a'+i)), Path: p, IsDir: true, Parent: m.root,
		})
		g.gates[p] = make(chan struct{})
	}
	for _, d := range dirs {
		m.enqueueLoad(d)
	}
	if len(m.executing) != projectTreeMaxConcurrentLoads {
		t.Fatalf("executing = %d, want %d", len(m.executing), projectTreeMaxConcurrentLoads)
	}
	if len(m.loadQueue) != 10-projectTreeMaxConcurrentLoads {
		t.Fatalf("queue = %d, want %d", len(m.loadQueue), 10-projectTreeMaxConcurrentLoads)
	}
	if got := g.callCount(); got != baseline {
		t.Fatalf("commands are lazy; nothing should have run yet, got %d calls (baseline %d)", got, baseline)
	}
}

func TestProjectTreeAsyncQueuedObsoleteJobsDropped(t *testing.T) {
	root := t.TempDir()
	g := &gatedReadDir{gates: map[string]chan struct{}{}}
	m := gatedTree(g)
	m.workspace = wsFor(root)

	// Fill all executing slots with blocked reads.
	fillers := make([]*projectTreeNode, projectTreeMaxConcurrentLoads)
	for i := range fillers {
		p := filepath.Join(root, "fill"+string(rune('a'+i)))
		fillers[i] = &projectTreeNode{Name: "fill", Path: p, IsDir: true, Expanded: true}
		g.gates[p] = make(chan struct{})
		m.enqueueLoad(fillers[i])
	}

	// A queued job whose node is then collapsed must never execute.
	victim := &projectTreeNode{
		Name: "victim", Path: filepath.Join(root, "victim"), IsDir: true, Expanded: true,
	}
	m.enqueueLoad(victim)
	if len(m.loadQueue) != 1 {
		t.Fatalf("expected 1 queued job, got %d", len(m.loadQueue))
	}
	m.collapseNode(victim)

	// A freed slot dispatches the queue — the victim must be skipped.
	m.executing = map[uint64]projectTreeLoadJob{} // pretend slots freed
	pumpTree(t, m, m.scheduleLoads())
	if g.called(victim.Path) {
		t.Fatal("queued obsolete job executed a read")
	}
}

func TestProjectTreeAsyncStaleRunningJobsReleaseSlots(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	g := &gatedReadDir{
		gates:   map[string]chan struct{}{rootA: make(chan struct{})},
		listing: map[string][]os.DirEntry{rootB: {fakeDirEntry{"b.txt", false}}},
	}
	m := gatedTree(g)

	blocked := runCmdAsync(m.SetWorkspace(wsFor(rootA)))
	// The switch releases the stale job's logical slot immediately: B's root
	// load must dispatch without waiting for A's wedged read.
	pumpTree(t, m, m.SetWorkspace(wsFor(rootB)))
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "b.txt" {
		t.Fatalf("workspace switch waited on the stale read: %+v", m.flatNodes)
	}
	g.release(rootA)
	<-blocked // drain the goroutine
}

// TestProjectTreePhysicalReadBoundAcrossGenerations is the audit-21
// regression: invalidation clears the *logical* executing set but must not
// multiply *physical* in-flight ReadDir calls — a dead generation's blocked
// reads hold their slots until they return, so a refresh storm can never run
// more than projectTreeMaxConcurrentLoads syscalls at once.
func TestProjectTreePhysicalReadBoundAcrossGenerations(t *testing.T) {
	root := t.TempDir()
	g := &gatedReadDir{gates: map[string]chan struct{}{}, listing: map[string][]os.DirEntry{}}
	started := make(chan string, 2*projectTreeMaxConcurrentLoads)
	m := NewProjectTree()
	m.readDir = func(path string) ([]os.DirEntry, error) {
		started <- path
		return g.read(path)
	}
	g.listing[root] = []os.DirEntry{}
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))
	<-started // the seeding root read — keep the channel gen-1-only

	// Fill every physical slot with a blocked generation-1 read. Gates must be
	// installed before the commands launch — g.read consults the map under its
	// own lock, so writes after dispatch would race it.
	gen1 := make([]string, projectTreeMaxConcurrentLoads)
	for i := range gen1 {
		p := filepath.Join(root, "g1"+string(rune('a'+i)))
		gen1[i] = p
		g.gates[p] = make(chan struct{})
	}
	gen1cmds := make([]<-chan tea.Msg, projectTreeMaxConcurrentLoads)
	for i, p := range gen1 {
		gen1cmds[i] = runCmdAsync(m.enqueueLoad(&projectTreeNode{
			Name: "g1", Path: p, IsDir: true, Expanded: true, Parent: m.root,
		}))
	}
	for range gen1 {
		p := <-started // barrier: all four gen-1 reads are kernel-blocked
		if filepath.Base(p)[:2] != "g1" {
			t.Fatalf("unexpected read start while filling slots: %s", p)
		}
	}

	// New generation: four fresh jobs issue, but none may start a read —
	// every physical slot is still held by a dead-generation syscall.
	m.invalidatePending()
	gen2 := make([]string, projectTreeMaxConcurrentLoads)
	gen2cmds := make([]<-chan tea.Msg, projectTreeMaxConcurrentLoads)
	for i := range gen2 {
		p := filepath.Join(root, "g2"+string(rune('a'+i)))
		gen2[i] = p
		gen2cmds[i] = runCmdAsync(m.enqueueLoad(&projectTreeNode{
			Name: "g2", Path: p, IsDir: true, Expanded: true, Parent: m.root,
		}))
	}
	select {
	case p := <-started:
		t.Fatalf("gen-2 read %s started while all physical slots were held by gen-1", p)
	case <-time.After(50 * time.Millisecond):
	}

	// Release the dead generation's reads one at a time; each freed slot lets
	// exactly one gen-2 command proceed — physical concurrency never exceeds
	// the cap.
	for _, p := range gen1 {
		g.release(p)
		<-started // exactly one gen-2 read begins per freed slot
	}
	if g.maxInflight != projectTreeMaxConcurrentLoads {
		t.Fatalf("maxInflight = %d, want exactly %d", g.maxInflight, projectTreeMaxConcurrentLoads)
	}

	// Deliver everything: gen-1 results discard on the generation check,
	// gen-2 results apply — the drop path is orthogonal to the slot release.
	// Receiving each result is the completion barrier: a command yields its
	// message only after readDir returned and the physical slot was released.
	for i := range gen1cmds {
		<-gen1cmds[i]
	}
	for i := range gen2cmds {
		deliver(t, m, <-gen2cmds[i])
	}
	if g.inflightCount() != 0 {
		t.Fatalf("leaked in-flight reads after all completions delivered: %d pending=%v", g.inflightCount(), g.inflightPaths())
	}
}

func TestProjectTreeAsyncContentVersionBumpsOnResult(t *testing.T) {
	g := &gatedReadDir{}
	m, _ := gatedSeededTree(t, g)
	before := m.ContentVersion()
	pumpTree(t, m, m.expandNode(m.flatNodes[0]))
	if m.ContentVersion() == before {
		t.Fatal("content version did not change after a load result applied")
	}
}

func TestProjectTreeAsyncHiddenFilesFilterAppliedAtResult(t *testing.T) {
	root := t.TempDir()
	g := &gatedReadDir{listing: map[string][]os.DirEntry{
		root: {fakeDirEntry{".secret", false}, fakeDirEntry{"visible.txt", false}},
	}}
	m := gatedTree(g)
	m.showHidden = false
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "visible.txt" {
		t.Fatalf("hidden file leaked into listing: %+v", m.flatNodes)
	}
}
