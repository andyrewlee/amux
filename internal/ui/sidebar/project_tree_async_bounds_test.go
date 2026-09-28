package sidebar

import (
	"os"
	"path/filepath"
	"testing"
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
