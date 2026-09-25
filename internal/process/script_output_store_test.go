package process

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// transcriptPath locates the envelope under the ws's primary (MetadataID) dir.
func transcriptPath(t *testing.T, root string, ws *data.Workspace) string {
	t.Helper()
	return filepath.Join(root, string(ws.MetadataID()), scriptTranscriptsFilename)
}

// TestRecordScriptOutput_WriteThrough asserts a recorded transcript lands on
// disk atomically under the workspace's metadata dir, with the same content
// the in-memory map holds.
func TestRecordScriptOutput_WriteThrough(t *testing.T) {
	metaRoot := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)
	ws := newHostedWorkspace(t, "nonconcurrent")

	runner.recordScriptOutput(ws, ScriptSetup, "setup did things", nil)

	path := transcriptPath(t, metaRoot, ws)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	var file scriptTranscriptsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("transcript file not JSON: %v", err)
	}
	if file.Version != scriptTranscriptsVersion {
		t.Fatalf("envelope version = %d, want %d", file.Version, scriptTranscriptsVersion)
	}
	entry, ok := file.Outputs[ScriptSetup]
	if !ok || entry.Text != "setup did things" {
		t.Fatalf("persisted outputs = %+v", file.Outputs)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("transcript perms = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
}

// TestLastScriptOutputs_RestartRead simulates a process restart: a fresh
// runner over the same metadata root serves the persisted transcript even
// though its memory map is empty.
func TestLastScriptOutputs_RestartRead(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")

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

	// A stale on-disk transcript (written by a "previous" process)…
	stale := scriptTranscriptsFile{
		Version: scriptTranscriptsVersion,
		Outputs: map[ScriptType]ScriptOutput{
			ScriptSetup: {Text: "stale disk copy", FinishedAt: time.Now().Add(-time.Hour)},
		},
	}
	raw, _ := json.Marshal(stale)
	dir := filepath.Dir(transcriptPath(t, metaRoot, ws))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcriptPath(t, metaRoot, ws), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// …and a fresh in-memory entry under the same key.
	runner.recordScriptOutput(ws, ScriptSetup, "fresh memory copy", nil)

	if got := runner.LastScriptOutputs(ws)[ScriptSetup].Text; got != "fresh memory copy" {
		t.Fatalf("memory should win over disk, got %q", got)
	}
}

// TestLastScriptOutputs_CorruptAndNewerRejected covers the two fail-closed
// read cases: unparseable content and a version the runner doesn't know.
func TestLastScriptOutputs_CorruptAndNewerRejected(t *testing.T) {
	metaRoot := t.TempDir()
	ws := newHostedWorkspace(t, "nonconcurrent")
	path := transcriptPath(t, metaRoot, ws)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	runner := NewScriptRunner(6200, 10)
	runner.SetTranscriptMetadataRoot(metaRoot)

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runner.LastScriptOutputs(ws); len(got) != 0 {
		t.Fatalf("corrupt envelope yielded %+v", got)
	}

	future, _ := json.Marshal(scriptTranscriptsFile{
		Version: scriptTranscriptsVersion + 1,
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
	dirs := runner.transcriptDirs(loaded)
	if len(dirs) < 2 {
		t.Fatalf("fixture produced only %d identity dirs; drift not engineered", len(dirs))
	}

	// Write the transcript under the LAST candidate dir — the drifted form.
	drifted := scriptTranscriptsFile{
		Version: scriptTranscriptsVersion,
		Outputs: map[ScriptType]ScriptOutput{
			ScriptArchive: {Text: "archive ran pre-drift", FinishedAt: time.Now()},
		},
	}
	raw, _ := json.Marshal(drifted)
	dir := dirs[len(dirs)-1]
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, scriptTranscriptsFilename), raw, 0o600); err != nil {
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
	if dirs := runner.transcriptDirs(ws); len(dirs) != 0 {
		t.Fatalf("transcriptDirs = %v, want nil without a root", dirs)
	}
	if got := runner.LastScriptOutputs(ws)[ScriptSetup].Text; got != "in-memory only" {
		t.Fatalf("in-memory read = %q", got)
	}
}
