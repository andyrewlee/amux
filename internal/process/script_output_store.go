package process

import (
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// Durability for lifecycle-script transcripts. recordScriptOutput writes
// through to <metadataRoot>/<workspaceID>/script-transcripts.json and
// LastScriptOutputs falls back to it on a memory miss, so the O viewer
// answers "why did setup fail" after a restart instead of going blank.
// Failure policy mirrors the in-memory map: persistence errors warn and are
// swallowed — a transcript write must never fail the script that produced
// it.
//
// The envelope I/O itself lives in data.ScriptTranscriptStore, which owns the
// schema and runs every write as one merge transaction under the workspace
// lock set — a restarted runner recording one hook cannot erase another
// process's persisted types, and corrupt/newer-schema files refuse the write
// rather than being overwritten.

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

// persistScriptOutputs merges this workspace's in-memory transcript set into
// the durable envelope. Writes are serialized on transcriptMu and the map is
// re-gathered at write time: two concurrent records (setup finishing while
// an on-done hook exits) would otherwise let an older snapshot overwrite a
// newer one, dropping the other type's entry. Gathering inside the lock plus
// the store's cross-process merge make every write complete by construction.
func (r *ScriptRunner) persistScriptOutputs(ws *data.Workspace) {
	if ws == nil {
		return
	}
	r.transcriptMu.Lock()
	defer r.transcriptMu.Unlock()
	r.mu.Lock()
	root := r.transcriptRoot
	outputs := r.outputsForLocked(ws)
	r.mu.Unlock()
	if root == "" {
		return
	}
	store := data.NewScriptTranscriptStore(root)
	merged := make(map[string]data.ScriptTranscript, len(outputs))
	for st, entry := range outputs {
		merged[string(st)] = entry
	}
	if err := store.WriteMerged(ws, merged); err != nil {
		logging.Warn("could not persist script transcripts workspace_root=%s error=%v", ws.Root, err)
	}
}

// loadScriptOutputs reads the workspace's persisted transcript set, merged
// across every identity-alias dir. Missing dirs/files and corrupt or
// newer-version envelopes all resolve to empty — the viewer degrades to
// "no output" rather than surfacing storage diagnostics.
func (r *ScriptRunner) loadScriptOutputs(ws *data.Workspace) map[ScriptType]ScriptOutput {
	out := map[ScriptType]ScriptOutput{}
	if ws == nil {
		return out
	}
	r.mu.Lock()
	root := r.transcriptRoot
	r.mu.Unlock()
	if root == "" {
		return out
	}
	for st, entry := range data.NewScriptTranscriptStore(root).ReadMerged(ws) {
		out[ScriptType(st)] = entry
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
