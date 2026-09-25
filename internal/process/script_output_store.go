package process

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/fsatomic"
	"github.com/andyrewlee/amux/internal/logging"
)

// Durability for lifecycle-script transcripts. recordScriptOutput writes
// through to <metadataRoot>/<workspaceID>/script-transcripts.json and
// LastScriptOutputs falls back to it on a memory miss, so the O viewer
// answers "why did setup fail" after a restart instead of going blank.
// Failure policy mirrors the in-memory map: persistence errors warn and are
// swallowed — a transcript write must never fail the script that produced it.

// scriptTranscriptsFilename names the per-workspace envelope holding the
// latest transcript per lifecycle script type.
const scriptTranscriptsFilename = "script-transcripts.json"

// scriptTranscriptsVersion is the newest envelope schema written. Readers
// reject anything newer rather than guessing at unknown fields — the same
// posture project-env.json takes.
const scriptTranscriptsVersion = 1

// scriptTranscriptsFile is the on-disk envelope: the latest transcript per
// script type, matching the in-memory map's granularity (latest run wins).
type scriptTranscriptsFile struct {
	Version int                         `json:"version"`
	Outputs map[ScriptType]ScriptOutput `json:"outputs"`
}

// SetTranscriptMetadataRoot installs the directory under which per-workspace
// transcript envelopes persist — the same workspaces-metadata root the store
// uses, so transcripts live and die with their workspace record. Empty or
// never called disables persistence (tests, embedded runners); the in-memory
// map keeps working unchanged.
func (r *ScriptRunner) SetTranscriptMetadataRoot(root string) {
	r.mu.Lock()
	r.transcriptRoot = root
	r.mu.Unlock()
}

// transcriptDirs returns the candidate metadata dirs for ws, in lookup order:
// the persisted identity first, then drifted forms that older artifacts may
// have been stamped under. Empty when persistence is disabled.
func (r *ScriptRunner) transcriptDirs(ws *data.Workspace) []string {
	r.mu.Lock()
	root := r.transcriptRoot
	r.mu.Unlock()
	if root == "" || ws == nil {
		return nil
	}
	ids := data.WorkspaceIdentitySet(ws)
	dirs := make([]string, 0, len(ids))
	for _, id := range ids {
		dirs = append(dirs, filepath.Join(root, string(id)))
	}
	return dirs
}

// persistScriptOutputs writes the workspace's full transcript set atomically.
// The dir is created with the store's 0700 and the file lands 0600 (CreateTemp
// semantics) — transcripts can embed anything the script printed.
//
// Writes are serialized on transcriptMu and the map is re-gathered at write
// time: two concurrent records (setup finishing while an on-done hook exits)
// would otherwise let an older snapshot overwrite a newer one, dropping the
// other type's entry. Gathering inside the lock makes the last writer's file
// complete by construction.
func (r *ScriptRunner) persistScriptOutputs(ws *data.Workspace) {
	dirs := r.transcriptDirs(ws)
	if len(dirs) == 0 {
		return
	}
	r.transcriptMu.Lock()
	defer r.transcriptMu.Unlock()
	r.mu.Lock()
	outputs := r.outputsForLocked(ws)
	r.mu.Unlock()
	dir := dirs[0]
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logging.Warn("could not create script transcript dir workspace_root=%s error=%v", ws.Root, err)
		return
	}
	env := scriptTranscriptsFile{Version: scriptTranscriptsVersion, Outputs: outputs}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		logging.Warn("could not marshal script transcripts workspace_root=%s error=%v", ws.Root, err)
		return
	}
	if err := fsatomic.WriteFile(filepath.Join(dir, scriptTranscriptsFilename), raw, 0o600); err != nil {
		logging.Warn("could not persist script transcripts workspace_root=%s error=%v", ws.Root, err)
	}
}

// loadScriptOutputs reads the workspace's persisted transcript set, trying
// each identity dir in order. Missing dirs/files and corrupt or newer-version
// envelopes all resolve to empty — the viewer degrades to "no output" rather
// than surfacing storage diagnostics.
func (r *ScriptRunner) loadScriptOutputs(ws *data.Workspace) map[ScriptType]ScriptOutput {
	out := map[ScriptType]ScriptOutput{}
	for _, dir := range r.transcriptDirs(ws) {
		raw, err := os.ReadFile(filepath.Join(dir, scriptTranscriptsFilename))
		if err != nil {
			continue
		}
		var file scriptTranscriptsFile
		if err := json.Unmarshal(raw, &file); err != nil {
			continue
		}
		if file.Version > scriptTranscriptsVersion {
			continue
		}
		// Merge across identity dirs rather than stopping at the first hit:
		// runs that straddled an ID drift can live under different dirs, and
		// per-type the fresher FinishedAt wins.
		for st, entry := range file.Outputs {
			if cur, ok := out[st]; !ok || entry.FinishedAt.After(cur.FinishedAt) {
				out[st] = entry
			}
		}
	}
	return out
}

// outputsForLocked gathers this workspace's in-memory transcripts (all script
// types) for the write-through envelope. Caller holds r.mu.
func (r *ScriptRunner) outputsForLocked(ws *data.Workspace) map[ScriptType]ScriptOutput {
	out := map[ScriptType]ScriptOutput{}
	prefix := scriptWorkspaceKey(ws) + "|"
	for key, entry := range r.lastOutput {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			out[ScriptType(rest)] = entry
		}
	}
	return out
}
