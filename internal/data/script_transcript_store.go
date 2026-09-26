package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// Durable lifecycle-script transcripts. Each workspace metadata dir may hold
// a script-transcripts.json envelope with the latest transcript per script
// type (setup/archive/on-done). Reads merge every identity-alias dir; writes
// run as one transaction under the workspace lock set so two amux processes
// recording at once can't drop each other's types.

// scriptTranscriptsFilename names the per-workspace envelope holding the
// latest transcript per lifecycle script type.
const scriptTranscriptsFilename = "script-transcripts.json"

// scriptTranscriptsVersion is the newest envelope schema written. Anything
// newer refuses both read and write rather than guessing at unknown fields —
// the same posture workspace.json takes.
const scriptTranscriptsVersion = 1

// ScriptTranscript is the durable tail of one lifecycle-script run: what the
// script emitted (bounded upstream) and how it finished. Err is the Wait
// error's text, empty on success. Field names are the on-disk v1 schema —
// keep them stable.
type ScriptTranscript struct {
	Text       string    `json:"text"`
	Err        string    `json:"error,omitempty"`
	FinishedAt time.Time `json:"finished_at"`
}

// scriptTranscriptsEnvelope is the on-disk shape: latest transcript per
// script type, keyed by the type's string form ("setup", "archive",
// "on-done") so the envelope carries no process-package vocabulary.
type scriptTranscriptsEnvelope struct {
	Version int                         `json:"version"`
	Outputs map[string]ScriptTranscript `json:"outputs"`
}

// ScriptTranscriptStore owns the script-transcripts.json envelope under each
// workspace's metadata dir. It rides the WorkspaceStore's per-workspace lock
// files — one lock protocol for everything under the metadata root — rather
// than carrying a second one. The store never mints workspace metadata: a
// write requires an existing workspace.json under the workspace's primary
// identity and no delete tombstone, so a late script hook cannot resurrect a
// removed record.
type ScriptTranscriptStore struct {
	workspaces *WorkspaceStore
}

// NewScriptTranscriptStore roots transcript storage at the same metadata
// root WorkspaceStore uses, so transcripts live and die with their workspace
// record.
func NewScriptTranscriptStore(root string) *ScriptTranscriptStore {
	return &ScriptTranscriptStore{workspaces: NewWorkspaceStore(root)}
}

// ReadMerged returns the latest transcript per script type across all of the
// workspace's identity-alias dirs — runs that straddled an ID drift can live
// under different dirs, and per type the fresher FinishedAt wins. Best
// effort and lock-free like the rest of the read path: missing dirs/files
// and corrupt or newer-version envelopes all resolve to empty rather than
// failing the caller.
func (s *ScriptTranscriptStore) ReadMerged(ws *Workspace) map[string]ScriptTranscript {
	out := map[string]ScriptTranscript{}
	if s == nil || ws == nil {
		return out
	}
	for _, id := range WorkspaceIdentitySet(ws) {
		env, ok := s.readEnvelope(s.transcriptDir(id))
		if !ok {
			continue
		}
		for st, entry := range env.Outputs {
			if cur, seen := out[st]; !seen || entry.FinishedAt.After(cur.FinishedAt) {
				out[st] = entry
			}
		}
	}
	return out
}

// WriteMerged merges outputs into the workspace's persisted transcripts and
// writes the result under the primary identity dir, as one transaction under
// the workspace identity lock set:
//
//   - every identity ID is locked (sorted order) before any file is read or
//     written, so a concurrent writer in another process serializes here;
//   - the write refuses — file left byte-identical — when the primary dir has
//     no valid workspace.json, a delete tombstone stands, or any candidate
//     envelope is unreadable, corrupt, or carries a newer schema version.
//     Diagnostic persistence fails conservatively rather than replacing
//     unknown data;
//   - per script type the fresher FinishedAt wins; an equal timestamp keeps
//     the already-persisted value, so map order never picks the winner;
//   - types the candidate lacks are preserved — a restarted runner recording
//     one hook can't erase the previous process's other transcripts.
//
// Alias-dir envelopes are merged but never deleted or rewritten.
func (s *ScriptTranscriptStore) WriteMerged(ws *Workspace, outputs map[string]ScriptTranscript) error {
	if s == nil || ws == nil {
		return errors.New("nil transcript store or workspace")
	}
	ids := WorkspaceIdentitySet(ws)
	if len(ids) == 0 {
		return errors.New("workspace has no identity")
	}
	locks, err := s.workspaces.lockWorkspaceIDs(ids...)
	if err != nil {
		return fmt.Errorf("lock workspace transcripts: %w", err)
	}
	defer unlockRegistryFiles(locks)

	primary := ws.MetadataID()
	if s.workspaces.IsDeleting(primary) {
		return fmt.Errorf("workspace %s is being deleted", primary)
	}
	if _, err := s.workspaces.load(primary, false); err != nil {
		return fmt.Errorf("workspace %s has no valid record: %w", primary, err)
	}

	merged := map[string]ScriptTranscript{}
	for _, id := range ids {
		env, err := s.readEnvelopeStrict(s.transcriptDir(id))
		if err != nil {
			return err
		}
		for st, entry := range env.Outputs {
			if cur, seen := merged[st]; !seen || entry.FinishedAt.After(cur.FinishedAt) {
				merged[st] = entry
			}
		}
	}
	for st, entry := range outputs {
		if cur, seen := merged[st]; !seen || entry.FinishedAt.After(cur.FinishedAt) {
			merged[st] = entry
		}
	}

	dir := s.transcriptDir(primary)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create transcript dir: %w", err)
	}
	raw, err := json.MarshalIndent(scriptTranscriptsEnvelope{
		Version: scriptTranscriptsVersion,
		Outputs: merged,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal transcripts: %w", err)
	}
	if err := fsatomic.WriteFile(filepath.Join(dir, scriptTranscriptsFilename), raw, 0o600); err != nil {
		return fmt.Errorf("write transcripts: %w", err)
	}
	return nil
}

// transcriptDir locates the metadata dir transcripts persist under for one
// identity form.
func (s *ScriptTranscriptStore) transcriptDir(id WorkspaceID) string {
	return filepath.Join(s.workspaces.root, string(id))
}

// readEnvelope decodes one dir's envelope for reads: anything absent,
// corrupt, or newer than this build resolves to !ok.
func (s *ScriptTranscriptStore) readEnvelope(dir string) (scriptTranscriptsEnvelope, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, scriptTranscriptsFilename))
	if err != nil {
		return scriptTranscriptsEnvelope{}, false
	}
	var env scriptTranscriptsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return scriptTranscriptsEnvelope{}, false
	}
	if env.Version > scriptTranscriptsVersion {
		return scriptTranscriptsEnvelope{}, false
	}
	return env, true
}

// readEnvelopeStrict decodes one dir's envelope inside a write transaction:
// missing is fine, but unreadable, corrupt, or newer-version content refuses
// the whole write so unknown data is never replaced.
func (s *ScriptTranscriptStore) readEnvelopeStrict(dir string) (scriptTranscriptsEnvelope, error) {
	raw, err := os.ReadFile(filepath.Join(dir, scriptTranscriptsFilename))
	if errors.Is(err, fs.ErrNotExist) {
		return scriptTranscriptsEnvelope{}, nil
	}
	if err != nil {
		return scriptTranscriptsEnvelope{}, fmt.Errorf("read transcripts in %s: %w", dir, err)
	}
	var env scriptTranscriptsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return scriptTranscriptsEnvelope{}, fmt.Errorf("existing transcripts in %s are unreadable: %w", dir, err)
	}
	if env.Version > scriptTranscriptsVersion {
		return scriptTranscriptsEnvelope{}, fmt.Errorf("existing transcripts in %s use newer schema v%d", dir, env.Version)
	}
	return env, nil
}
