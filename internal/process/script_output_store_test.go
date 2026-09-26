package process

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// testTranscriptsFilename mirrors the data-owned envelope name for fixture
// construction. The store's copy is authoritative; duplicating the literal
// keeps process tests honest about the on-disk contract.
const testTranscriptsFilename = "script-transcripts.json"

// testTranscriptsEnvelope is the on-disk v1 shape, for writing fixtures the
// public store API cannot produce (drifted-dir envelopes, future versions).
type testTranscriptsEnvelope struct {
	Version int                         `json:"version"`
	Outputs map[ScriptType]ScriptOutput `json:"outputs"`
}

// transcriptPath locates the envelope under the ws's primary (MetadataID) dir.
func transcriptPath(t *testing.T, root string, ws *data.Workspace) string {
	t.Helper()
	return filepath.Join(root, string(ws.MetadataID()), testTranscriptsFilename)
}

// saveWorkspaceRecord persists ws under metaRoot so transcript writes see a
// real workspace.json — WriteMerged refuses to stamp transcripts for a
// record that does not exist, so persistence fixtures must be real.
func saveWorkspaceRecord(t *testing.T, metaRoot string, ws *data.Workspace) {
	t.Helper()
	store := data.NewWorkspaceStore(metaRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save fixture workspace: %v", err)
	}
}

// TestRecordScriptOutput_WriteThrough asserts a recorded transcript lands on
// disk atomically under the workspace's metadata dir, with the same content
// the in-memory map holds.
func TestRecordScriptOutput_WriteThrough(t *testing.T) {
	metaRoot := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)

	runner.recordScriptOutput(ws, ScriptSetup, "setup did things", nil)

	path := transcriptPath(t, metaRoot, ws)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	var file testTranscriptsEnvelope
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("transcript file not JSON: %v", err)
	}
	if file.Version != 1 {
		t.Fatalf("envelope version = %d, want 1", file.Version)
	}
	entry, ok := file.Outputs[ScriptSetup]
	if !ok || entry.Text != "setup did things" {
		t.Fatalf("persisted outputs = %+v", file.Outputs)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("transcript perms = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
}

// TestRecordScriptOutput_UnsavedWorkspaceRefuses pins the no-resurrection
// boundary on the runner path: a workspace with no persisted record gets a
// warning and no file, and the in-memory transcript still lands.
func TestRecordScriptOutput_UnsavedWorkspaceRefuses(t *testing.T) {
	metaRoot := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	ws := newHostedWorkspace(t, "nonconcurrent")

	runner.recordScriptOutput(ws, ScriptSetup, "setup did things", nil)

	if _, err := os.Stat(transcriptPath(t, metaRoot, ws)); !os.IsNotExist(err) {
		t.Fatalf("transcript file exists for an unsaved workspace (err %v)", err)
	}
	if got := runner.LastScriptOutputs(ws)[ScriptSetup].Text; got != "setup did things" {
		t.Fatalf("in-memory record = %q, want it kept despite the refused write", got)
	}
}

// TestRecordScriptOutput_RestartPreservesOtherTypes is the plan-038
// regression: a restarted runner has an empty memory map, so recording one
// type must merge with — not overwrite — the persisted envelope.
func TestRecordScriptOutput_RestartPreservesOtherTypes(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)

	first := NewScriptRunner(6200, 10)
	first.SetTranscriptMetadataRoot(metaRoot)
	first.recordScriptOutput(ws, ScriptSetup, "pre-restart setup tail", nil)

	// "Restart": a fresh runner over the same root records a different type.
	second := NewScriptRunner(6200, 10)
	second.SetTranscriptMetadataRoot(metaRoot)
	second.recordScriptOutput(ws, ScriptOnDone, "post-restart hook tail", nil)

	raw, err := os.ReadFile(transcriptPath(t, metaRoot, ws))
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	var file testTranscriptsEnvelope
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("transcript file not JSON: %v", err)
	}
	if got := file.Outputs[ScriptSetup].Text; got != "pre-restart setup tail" {
		t.Fatalf("restart write lost the setup transcript: %+v", file.Outputs)
	}
	if got := file.Outputs[ScriptOnDone].Text; got != "post-restart hook tail" {
		t.Fatalf("restart write lost the on-done transcript: %+v", file.Outputs)
	}
}

// TestRecordScriptOutput_NewerSchemaRefuses asserts the fail-closed write
// posture: an envelope from a newer amux must not be overwritten with v1.
func TestRecordScriptOutput_NewerSchemaRefuses(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)
	path := transcriptPath(t, metaRoot, ws)

	future, _ := json.Marshal(testTranscriptsEnvelope{
		Version: 2,
		Outputs: map[ScriptType]ScriptOutput{ScriptSetup: {Text: "from the future"}},
	})
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatal(err)
	}

	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	runner.recordScriptOutput(ws, ScriptArchive, "v1 write attempt", nil)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	if string(raw) != string(future) {
		t.Fatal("a newer-schema envelope was overwritten instead of refused")
	}
}

// TestRecordScriptOutput_CorruptPreserved asserts the same for unreadable
// content: the write refuses and the file stays byte-identical — a later
// repair or reader still gets the evidence.
func TestRecordScriptOutput_CorruptPreserved(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)
	path := transcriptPath(t, metaRoot, ws)

	corrupt := []byte("{not json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	runner.recordScriptOutput(ws, ScriptSetup, "write attempt", nil)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	if string(raw) != string(corrupt) {
		t.Fatal("a corrupt envelope was overwritten instead of preserved")
	}
}

// TestLastScriptOutputs_RestartRead simulates a process restart: a fresh
// runner over the same metadata root serves the persisted transcript even
// though its memory map is empty.
func TestLastScriptOutputs_RestartRead(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)

	first := NewScriptRunner(6200, 10)
	first.SetTranscriptMetadataRoot(metaRoot)
	first.recordScriptOutput(ws, ScriptSetup, "first-run transcript", nil)

	second := NewScriptRunner(6200, 10)
	second.SetTranscriptMetadataRoot(metaRoot)
	got := second.LastScriptOutputs(ws)
	if got[ScriptSetup].Text != "first-run transcript" {
		t.Fatalf("post-restart outputs = %+v", got)
	}
}

// TestLastScriptOutputs_MemoryWinsOverDisk asserts a fresher in-memory entry
// is not shadowed by a stale persisted one.
func TestLastScriptOutputs_MemoryWinsOverDisk(t *testing.T) {
	metaRoot := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)

	// A stale on-disk transcript (written by a "previous" process)…
	stale := testTranscriptsEnvelope{
		Version: 1,
		Outputs: map[ScriptType]ScriptOutput{
			ScriptSetup: {Text: "stale disk copy", FinishedAt: time.Now().Add(-time.Hour)},
		},
	}
	raw, _ := json.Marshal(stale)
	if err := os.WriteFile(transcriptPath(t, metaRoot, ws), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// …and a fresh in-memory entry under the same key.
	runner.recordScriptOutput(ws, ScriptSetup, "fresh memory copy", nil)

	if got := runner.LastScriptOutputs(ws)[ScriptSetup].Text; got != "fresh memory copy" {
		t.Fatalf("memory should win over disk, got %q", got)
	}
}

// TestLastScriptOutputs_RecordDuringHydrationWins pins the freshness-safe
// fold: a transcript recorded in the window between the disk read and the
// memory fold must survive — the stale pre-read copy can't clobber it.
func TestLastScriptOutputs_RecordDuringHydrationWins(t *testing.T) {
	metaRoot := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)

	// Persist only a stale archive tail, so the hydration pass has work.
	stale := testTranscriptsEnvelope{
		Version: 1,
		Outputs: map[ScriptType]ScriptOutput{
			ScriptArchive: {Text: "stale disk archive", FinishedAt: time.Now().Add(-time.Hour)},
		},
	}
	raw, _ := json.Marshal(stale)
	if err := os.WriteFile(transcriptPath(t, metaRoot, ws), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// The seam fires inside LastScriptOutputs between the disk read and the
	// fold — exactly where a racing record used to be overwritten.
	ran := false
	runner.transcriptLoadHook = func() {
		if ran {
			return
		}
		ran = true
		runner.recordScriptOutput(ws, ScriptArchive, "racing fresh archive", nil)
	}

	got := runner.LastScriptOutputs(ws)
	if !ran {
		t.Fatal("the load seam never ran — test exercises nothing")
	}
	if got[ScriptArchive].Text != "racing fresh archive" {
		t.Fatalf("hydration clobbered the racing record: %q", got[ScriptArchive].Text)
	}
	if mem := runner.lastOutput[scriptOutputKey(ws, ScriptArchive)]; mem.Text != "racing fresh archive" {
		t.Fatalf("folded memory entry = %q, want the racing record", mem.Text)
	}
}

// TestLastScriptOutputs_CorruptAndNewerRejected covers the two fail-closed
// read cases: unparseable content and a version the runner doesn't know.
func TestLastScriptOutputs_CorruptAndNewerRejected(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")
	saveWorkspaceRecord(t, metaRoot, ws)
	path := transcriptPath(t, metaRoot, ws)

	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runner.LastScriptOutputs(ws); len(got) != 0 {
		t.Fatalf("corrupt envelope yielded %+v", got)
	}

	future, _ := json.Marshal(testTranscriptsEnvelope{
		Version: 2,
		Outputs: map[ScriptType]ScriptOutput{ScriptSetup: {Text: "future"}},
	})
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runner.LastScriptOutputs(ws); len(got) != 0 {
		t.Fatalf("newer-version envelope yielded %+v", got)
	}
}

// TestLastScriptOutputs_DriftedIdentityDir writes the transcript under a
// non-primary identity form and proves the identity-set fallback still finds
// it. The fixture engineers a real drift: the workspace is saved while its
// root is a live symlink (store key minted from the resolved path), then the
// link is removed so ComputedID hashes the unresolved form.
func TestLastScriptOutputs_DriftedIdentityDir(t *testing.T) {
	metaRoot := t.TempDir()
	repo := t.TempDir()
	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "wslink")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	store := data.NewWorkspaceStore(metaRoot)
	ws := data.NewWorkspace("feature", "feature", "main", repo, link)
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Remove the link so ComputedID resolves the link verbatim — a different
	// identity than the store key minted while it resolved to realRoot.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}

	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	ids := data.WorkspaceIdentitySet(loaded)
	if len(ids) < 2 {
		t.Fatalf("fixture produced only %d identity forms; drift not engineered", len(ids))
	}

	// Write the transcript under the LAST identity form — the drifted one.
	drifted := testTranscriptsEnvelope{
		Version: 1,
		Outputs: map[ScriptType]ScriptOutput{
			ScriptArchive: {Text: "archive ran pre-drift", FinishedAt: time.Now()},
		},
	}
	raw, _ := json.Marshal(drifted)
	dir := filepath.Join(metaRoot, string(ids[len(ids)-1]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, testTranscriptsFilename), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	got := runner.LastScriptOutputs(loaded)
	if got[ScriptArchive].Text != "archive ran pre-drift" {
		t.Fatalf("drifted-dir transcript not found: %+v", got)
	}
}

// TestScriptTranscripts_DisabledWithoutRoot pins the zero-dep posture: no
// metadata root means the in-memory map is the whole story and nothing
// touches the filesystem.
func TestScriptTranscripts_DisabledWithoutRoot(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")

	runner.recordScriptOutput(ws, ScriptSetup, "in-memory only", nil)
	if got := runner.loadScriptOutputs(ws); len(got) != 0 {
		t.Fatalf("loadScriptOutputs = %v, want empty without a root", got)
	}
	if got := runner.LastScriptOutputs(ws)[ScriptSetup].Text; got != "in-memory only" {
		t.Fatalf("in-memory read = %q", got)
	}
}
