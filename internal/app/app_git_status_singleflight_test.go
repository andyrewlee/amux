package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func singleflightApp(stub *fileWatcherGitStatusStub) *App {
	return &App{
		gitStatus: stub,
		dashboard: dashboard.New(),
		sidebar:   sidebar.NewTabbedSidebar(),
	}
}

// TestGitStatusDedup_BurstCoalesces: N requests for one root while a refresh
// is in flight produce zero new cmds; the landing result emits exactly one
// follow-up, and that follow-up's landing ends the cycle.
func TestGitStatusDedup_BurstCoalesces(t *testing.T) {
	stub := &fileWatcherGitStatusStub{}
	app := singleflightApp(stub)
	root := "/repo/ws"

	first := app.requestGitStatus(root)
	if first == nil {
		t.Fatal("first request must produce a refresh cmd")
	}
	for i := 0; i < 5; i++ {
		if cmd := app.requestGitStatus(root); cmd != nil {
			t.Fatalf("request %d while in-flight must coalesce, got a cmd", i+2)
		}
	}

	msg := first()
	tracked, ok := msg.(messages.GitStatusResult)
	if !ok || !tracked.Tracked {
		t.Fatalf("expected tracked GitStatusResult, got %T %+v", msg, msg)
	}
	if len(stub.refreshFastRoots) != 1 {
		t.Fatalf("refreshes = %v, want exactly 1", stub.refreshFastRoots)
	}

	followUp := app.gitStatusFollowUp(tracked.Root)
	if followUp == nil {
		t.Fatal("pending requests must drain into exactly one follow-up")
	}
	followUp()
	if len(stub.refreshFastRoots) != 2 {
		t.Fatalf("refreshes = %v, want in-flight + one coalesced", stub.refreshFastRoots)
	}
	if cmd := app.gitStatusFollowUp(root); cmd != nil {
		t.Fatal("drained pending must not spawn another follow-up")
	}
}

// TestGitStatusDedup_FullEscalation: a full-status request pending behind a
// fast refresh must drain as a full refresh — never satisfied by fast.
func TestGitStatusDedup_FullEscalation(t *testing.T) {
	stub := &fileWatcherGitStatusStub{}
	app := singleflightApp(stub)
	root := "/repo/ws"

	first := app.requestGitStatus(root)
	first()
	if cmd := app.requestGitStatus(root); cmd != nil {
		t.Fatal("fast request while in-flight must coalesce")
	}
	if cmd := app.requestGitStatusFull(root); cmd != nil {
		t.Fatal("full request while in-flight must coalesce")
	}

	followUp := app.gitStatusFollowUp(root)
	if followUp == nil {
		t.Fatal("pending must drain")
	}
	followUp()
	if len(stub.refreshRoots) != 1 || len(stub.refreshFastRoots) != 1 {
		t.Fatalf("full escalation failed: refresh=%v fast=%v", stub.refreshRoots, stub.refreshFastRoots)
	}
}

// TestGitStatusDedup_RootsAreIndependent: dedup keys per root.
func TestGitStatusDedup_RootsAreIndependent(t *testing.T) {
	stub := &fileWatcherGitStatusStub{}
	app := singleflightApp(stub)

	if app.requestGitStatus("/a") == nil {
		t.Fatal("first /a request must run")
	}
	if app.requestGitStatus("/b") == nil {
		t.Fatal("/b must not be blocked by /a's in-flight refresh")
	}
	if app.requestGitStatus("/a") != nil {
		t.Fatal("/a must coalesce with its own in-flight refresh")
	}
}

// TestGitStatusDedup_CachedResultDoesNotClearInFlight: a cache-hit result for
// the same root must not end the real refresh's dedup window.
func TestGitStatusDedup_CachedResultDoesNotClearInFlight(t *testing.T) {
	stub := &fileWatcherGitStatusStub{
		cacheByRoot: map[string]*git.StatusResult{"/repo/ws": {Clean: true}},
	}
	app := singleflightApp(stub)
	root := "/repo/ws"

	inFlight := app.requestGitStatus(root)
	if inFlight == nil {
		t.Fatal("refresh must start")
	}
	cached := app.requestGitStatusCached(root, false)
	if cached == nil {
		t.Fatal("cache hit must serve a result")
	}
	msg := cached()
	res, ok := msg.(messages.GitStatusResult)
	if !ok || res.Tracked {
		t.Fatalf("cached result must be untracked, got %+v", msg)
	}
	// The untracked result reaching the handler must not disturb the dedup
	// state: the in-flight mark stands, so a further request still coalesces.
	if cmd := app.requestGitStatus(root); cmd != nil {
		t.Fatal("untracked cache result must not clear the in-flight mark")
	}
	inFlight()
	if cmd := app.gitStatusFollowUp(root); cmd == nil {
		t.Fatal("the coalesced request must still drain after the real result")
	}
}

// TestGitStatusDedup_BatchMarksInFlight: roots already being refreshed join
// the pending set instead of the batch; the batch result drains them.
func TestGitStatusDedup_BatchMarksInFlight(t *testing.T) {
	stub := &fileWatcherGitStatusStub{}
	app := singleflightApp(stub)

	app.requestGitStatus("/a") // in-flight
	batch := app.requestGitStatusBatch([]string{"/a", "/b", "/c"})
	if batch == nil {
		t.Fatal("batch must run for the unblocked roots")
	}
	msg := batch()
	res, ok := msg.(messages.GitStatusBatchResult)
	if !ok {
		t.Fatalf("expected GitStatusBatchResult, got %T", msg)
	}
	if len(res.Results) != 2 {
		t.Fatalf("batch ran %d roots, want 2 (b,c — a was in-flight)", len(res.Results))
	}
	// /a's pending flag drains when its own refresh lands.
	if cmd := app.gitStatusFollowUp("/a"); cmd == nil {
		t.Fatal("/a's batched request must produce a follow-up")
	}
	// /b and /c were refreshed by the batch — no pending, no follow-up.
	for _, root := range []string{"/b", "/c"} {
		if cmd := app.gitStatusFollowUp(root); cmd != nil {
			t.Fatalf("%s must not produce a follow-up", root)
		}
	}
}

// TestGitStatusRequestMessage_RoutesThroughDedup: UI models request status
// via messages.GitStatusRequest (055 layering) — dispatch must run the same
// in-flight dedup path as internal callers, in full mode (line stats).
func TestGitStatusRequestMessage_RoutesThroughDedup(t *testing.T) {
	stub := &fileWatcherGitStatusStub{}
	app := singleflightApp(stub)
	root := "/repo/ws"

	_, cmd := app.Update(messages.GitStatusRequest{Root: root})
	if cmd == nil {
		t.Fatal("GitStatusRequest must produce a refresh cmd")
	}
	msg := cmd()
	res, ok := msg.(messages.GitStatusResult)
	if !ok || !res.Tracked {
		t.Fatalf("expected tracked GitStatusResult, got %T %+v", msg, msg)
	}
	if len(stub.refreshRoots) != 1 || stub.refreshRoots[0] != root {
		t.Fatalf("full refreshes = %v, want [%s]", stub.refreshRoots, root)
	}

	// A second request while in-flight coalesces — no cmd, pending marked.
	app2stub := &fileWatcherGitStatusStub{}
	app2 := singleflightApp(app2stub)
	app2.requestGitStatusFull(root) // in-flight
	_, cmd = app2.Update(messages.GitStatusRequest{Root: root})
	if cmd != nil {
		t.Fatal("request while in-flight must coalesce, got a cmd")
	}
	if !app2.gitStatusPending[root] || !app2.gitStatusPendingFull[root] {
		t.Fatalf("coalesced request must mark pending+full, got pending=%v full=%v",
			app2.gitStatusPending[root], app2.gitStatusPendingFull[root])
	}
}
