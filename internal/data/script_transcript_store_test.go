package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// transcriptFixture saves a real workspace record under root and returns the
// store's stamped copy — WriteMerged requires an existing workspace.json
// under the primary identity, so fixtures must be real.
func transcriptFixture(t *testing.T, root string) *Workspace {
	t.Helper()
	ws := NewWorkspace("feature", "feature", "main", t.TempDir(), t.TempDir())
	if err := NewWorkspaceStore(root).Save(ws); err != nil {
		t.Fatalf("save fixture workspace: %v", err)
	}
	loaded, err := NewWorkspaceStore(root).Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("load fixture workspace: %v", err)
	}
	return loaded
}

func transcriptEnvelopePath(root string, ws *Workspace) string {
	return filepath.Join(root, string(ws.MetadataID()), scriptTranscriptsFilename)
}

func readEnvelopeFile(t *testing.T, path string) scriptTranscriptsEnvelope {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	var env scriptTranscriptsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("transcript file not JSON: %v", err)
	}
	return env
}

// TestScriptTranscriptStore_WriteThenRead covers the happy path: a write
// lands under the primary identity with private perms and ReadMerged serves
// it back.
func TestScriptTranscriptStore_WriteThenRead(t *testing.T) {
	root := t.TempDir()
	ws := transcriptFixture(t, root)
	store := NewScriptTranscriptStore(root)

	err := store.WriteMerged(ws, map[string]ScriptTranscript{
		"setup": {Text: "setup tail", FinishedAt: time.Now()},
	})
	if err != nil {
		t.Fatalf("WriteMerged: %v", err)
	}

	path := transcriptEnvelopePath(root, ws)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("transcript perms = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
	got := store.ReadMerged(ws)
	if got["setup"].Text != "setup tail" {
		t.Fatalf("ReadMerged = %+v", got)
	}
}

// TestScriptTranscriptStore_TwoStoresMerge is the plan-038 regression: two
// independent stores (two amux processes) recording different types must
// merge instead of last-writer-overwrites.
func TestScriptTranscriptStore_TwoStoresMerge(t *testing.T) {
	root := t.TempDir()
	ws := transcriptFixture(t, root)

	first := NewScriptTranscriptStore(root)
	second := NewScriptTranscriptStore(root)

	if err := first.WriteMerged(ws, map[string]ScriptTranscript{
		"setup": {Text: "setup tail", FinishedAt: time.Now()},
	}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// second's caller only ever saw its own run — the merge must keep setup.
	if err := second.WriteMerged(ws, map[string]ScriptTranscript{
		"archive": {Text: "archive tail", FinishedAt: time.Now()},
	}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	env := readEnvelopeFile(t, transcriptEnvelopePath(root, ws))
	if env.Outputs["setup"].Text != "setup tail" {
		t.Fatalf("second store's write lost the setup transcript: %+v", env.Outputs)
	}
	if env.Outputs["archive"].Text != "archive tail" {
		t.Fatalf("second store's write lost the archive transcript: %+v", env.Outputs)
	}
}

// TestScriptTranscriptStore_FresherFinishedAtWins asserts the merge resolves
// a same-type conflict by recency, deterministically.
func TestScriptTranscriptStore_FresherFinishedAtWins(t *testing.T) {
	root := t.TempDir()
	ws := transcriptFixture(t, root)
	store := NewScriptTranscriptStore(root)
	old := time.Now().Add(-time.Hour)

	if err := store.WriteMerged(ws, map[string]ScriptTranscript{
		"setup": {Text: "newer", FinishedAt: old.Add(2 * time.Hour)},
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	// An older candidate must not displace the persisted newer entry.
	if err := store.WriteMerged(ws, map[string]ScriptTranscript{
		"setup": {Text: "older", FinishedAt: old},
	}); err != nil {
		t.Fatalf("stale write: %v", err)
	}
	if got := store.ReadMerged(ws)["setup"].Text; got != "newer" {
		t.Fatalf("older write displaced the newer transcript: %q", got)
	}
}

// TestScriptTranscriptStore_NoRecordRefuses pins the no-resurrection
// boundary: no workspace.json under the primary identity, no write.
func TestScriptTranscriptStore_NoRecordRefuses(t *testing.T) {
	root := t.TempDir()
	ws := NewWorkspace("ghost", "ghost", "main", t.TempDir(), t.TempDir())

	err := NewScriptTranscriptStore(root).WriteMerged(ws, map[string]ScriptTranscript{
		"setup": {Text: "late hook", FinishedAt: time.Now()},
	})
	if err == nil {
		t.Fatal("write for an unrecorded workspace succeeded")
	}
	if _, statErr := os.Stat(transcriptEnvelopePath(root, ws)); !os.IsNotExist(statErr) {
		t.Fatal("refused write still created the transcript file")
	}
}

// TestScriptTranscriptStore_TombstoneRefuses asserts a workspace mid-delete
// cannot gain transcripts — a late script hook must not recreate artifacts
// under a record being removed.
func TestScriptTranscriptStore_TombstoneRefuses(t *testing.T) {
	root := t.TempDir()
	ws := transcriptFixture(t, root)
	workspaces := NewWorkspaceStore(root)
	if err := workspaces.MarkDeleting(ws.MetadataID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}

	err := NewScriptTranscriptStore(root).WriteMerged(ws, map[string]ScriptTranscript{
		"on-done": {Text: "late hook", FinishedAt: time.Now()},
	})
	if err == nil {
		t.Fatal("write under a delete tombstone succeeded")
	}
	if _, statErr := os.Stat(transcriptEnvelopePath(root, ws)); !os.IsNotExist(statErr) {
		t.Fatal("refused write still created the transcript file")
	}
}

// TestScriptTranscriptStore_CorruptAndNewerRefuse covers the fail-closed
// write postures: the existing file stays byte-identical whether it is
// unparseable or carries a schema this build doesn't know.
func TestScriptTranscriptStore_CorruptAndNewerRefuse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{"corrupt", []byte("{not json")},
		{"newer schema", []byte(`{"version":2,"outputs":{"setup":{"text":"future"}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ws := transcriptFixture(t, root)
			path := transcriptEnvelopePath(root, ws)
			if err := os.WriteFile(path, tc.content, 0o600); err != nil {
				t.Fatal(err)
			}

			err := NewScriptTranscriptStore(root).WriteMerged(ws, map[string]ScriptTranscript{
				"archive": {Text: "write attempt", FinishedAt: time.Now()},
			})
			if err == nil {
				t.Fatalf("write over %s content succeeded", tc.name)
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("transcript file missing: %v", readErr)
			}
			if string(raw) != string(tc.content) {
				t.Fatalf("the %s envelope was overwritten instead of preserved", tc.name)
			}
		})
	}
}

// TestScriptTranscriptStore_ReadMergedSkipsBadEnvelopes asserts the read side
// keeps its degrade-to-empty posture even across identity-alias dirs: a bad
// primary envelope doesn't hide a readable alias one.
func TestScriptTranscriptStore_ReadMergedSkipsBadEnvelopes(t *testing.T) {
	root := t.TempDir()
	ws := transcriptFixture(t, root)
	store := NewScriptTranscriptStore(root)

	ids := WorkspaceIdentitySet(ws)
	if len(ids) < 1 {
		t.Fatal("fixture produced no identity forms")
	}
	if err := os.WriteFile(transcriptEnvelopePath(root, ws), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Seed a readable envelope under a different identity form if one exists;
	// either way the corrupt primary alone must yield empty, not error.
	if len(ids) > 1 {
		alias := filepath.Join(root, string(ids[len(ids)-1]))
		if err := os.MkdirAll(alias, 0o700); err != nil {
			t.Fatal(err)
		}
		good, _ := json.Marshal(scriptTranscriptsEnvelope{
			Version: 1,
			Outputs: map[string]ScriptTranscript{
				"archive": {Text: "alias tail", FinishedAt: time.Now()},
			},
		})
		if err := os.WriteFile(filepath.Join(alias, scriptTranscriptsFilename), good, 0o600); err != nil {
			t.Fatal(err)
		}
		got := store.ReadMerged(ws)
		if got["archive"].Text != "alias tail" {
			t.Fatalf("alias-dir transcript not found: %+v", got)
		}
		return
	}
	if got := store.ReadMerged(ws); len(got) != 0 {
		t.Fatalf("corrupt envelope yielded %+v", got)
	}
}
