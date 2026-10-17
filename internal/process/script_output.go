package process

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// scriptOutputTailBytes bounds each recorded lifecycle-script transcript.
// Output is for diagnosis ("why did setup fail"), not archival — the last
// 64 KiB is what matters when a script dies, and an unbounded buffer on a
// chatty setup would grow for the life of the runner.
const scriptOutputTailBytes = 64 << 10

// tailWriter is a bounded io.Writer keeping only the most recent bytes.
// Setup/archive/on-done attach it as both Stdout and Stderr — os/exec
// dedupes an identical writer into one copy goroutine (the CombinedOutput
// contract), so a single buffer is safe and preserves real output order.
// The mutex exists for LiveSetupOutput: an in-flight setup's tail is read
// by the viewer while the copier goroutine is still writing it.
type tailWriter struct {
	mu      sync.Mutex
	buf     []byte
	max     int
	dropped int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		excess := len(w.buf) - w.max
		// Copy (not reslice) so the dropped prefix's capacity is released.
		w.buf = append([]byte(nil), w.buf[excess:]...)
		w.dropped += excess
	}
	return len(p), nil
}

// String renders the tail with a truncation note when earlier output was
// dropped — callers fold this verbatim into errors and the output dialog.
func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dropped > 0 {
		return fmt.Sprintf("[... %d bytes of earlier output dropped ...]\n%s", w.dropped, w.buf)
	}
	return string(w.buf)
}

// ScriptOutput is the recorded tail of one lifecycle-script run — what the
// script emitted and how it finished. Err is the Wait error's text
// (empty on success). It aliases the data package's durable transcript
// record: the persisted envelope schema (script_output_store.go) is owned
// there, and the shared type keeps the JSON field names stable.
type ScriptOutput = data.ScriptTranscript

// scriptOutputKey scopes the record to a workspace+type so a workspace's
// last setup, archive, and on-done transcripts coexist.
func scriptOutputKey(ws *data.Workspace, scriptType ScriptType) string {
	return scriptWorkspaceKey(ws) + "|" + string(scriptType)
}

// recordScriptOutput stores the latest transcript for the workspace+type.
// Successful runs with no output are not recorded — an empty transcript
// would only render as a blank section in the viewer.
func (r *ScriptRunner) recordScriptOutput(ws *data.Workspace, scriptType ScriptType, text string, runErr error) {
	if text == "" && runErr == nil {
		return
	}
	entry := ScriptOutput{Text: text, FinishedAt: time.Now()}
	if runErr != nil {
		entry.Err = runErr.Error()
	}
	// scriptOutputKey resolves symlinks — compute it outside r.mu so a
	// stalled resolution can't extend the lock's hold.
	key := scriptOutputKey(ws, scriptType)
	r.mu.Lock()
	r.lastOutput[key] = entry
	r.mu.Unlock()
	// Write-through outside r.mu: the fs write must not extend its hold time,
	// and its own errors degrade to warnings inside.
	r.persistScriptOutputs(ws)
}

// LastScriptOutputs returns every recorded lifecycle transcript for the
// workspace, keyed by script type — the composite the output viewer shows.
// Run output is deliberately absent: it has its own live-tail surface
// (RunScriptOutput). The map is a copy; entries survive workspace deletion
// until overwritten — bounded by the workspace count, same as `running`.
func (r *ScriptRunner) LastScriptOutputs(ws *data.Workspace) map[ScriptType]ScriptOutput {
	out := map[ScriptType]ScriptOutput{}
	if ws == nil {
		return out
	}
	prefix := scriptWorkspaceKey(ws) + "|"
	r.mu.Lock()
	for key, entry := range r.lastOutput {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			out[ScriptType(rest)] = entry
		}
	}
	r.mu.Unlock()
	// Disk fallback fills types memory lacks. The fold re-reads lastOutput
	// under r.mu and compares FinishedAt rather than trusting the pre-read
	// copy — a record landing between the disk read and here is newer and
	// must survive hydration.
	if missing := missingScriptTypes(out); len(missing) > 0 {
		disk := r.loadScriptOutputs(ws)
		if hook := r.transcriptLoadHook; hook != nil {
			hook()
		}
		// scriptOutputKey resolves symlinks — precompute per-type keys
		// outside r.mu, matching the prefix hoist above.
		keys := make(map[ScriptType]string, len(disk))
		for st := range disk {
			keys[st] = scriptOutputKey(ws, st)
		}
		r.mu.Lock()
		for st, entry := range disk {
			key := keys[st]
			cur, have := r.lastOutput[key]
			if !have || cur.FinishedAt.Before(entry.FinishedAt) {
				r.lastOutput[key] = entry
				cur = entry
			}
			if prev, seen := out[st]; !seen || prev.FinishedAt.Before(cur.FinishedAt) {
				out[st] = cur
			}
		}
		r.mu.Unlock()
	}
	return out
}

// LiveSetupOutput returns the in-flight setup's captured tail — text so far,
// and whether a setup sequence is still running for the workspace. The
// script-output viewer polls it to render a live "setup (running)" section;
// ("", false) means no setup is in flight and the recorded transcript in
// LastScriptOutputs is the freshest state.
func (r *ScriptRunner) LiveSetupOutput(ws *data.Workspace) (string, bool) {
	if ws == nil {
		return "", false
	}
	key := scriptWorkspaceKey(ws)
	r.mu.Lock()
	tail := r.liveSetup[key]
	r.mu.Unlock()
	if tail == nil {
		return "", false
	}
	return tail.String(), true
}

// ForgetWorkspace drops every retained record keyed to the workspace:
// lastOutput transcripts (the in-memory copies — disk transcripts are the
// metadata store's lifecycle, swept with it), the runSessionsSeen/Swept
// identity forms, a parked pendingRelease entry, and a quiescent
// lifecycle-coordinator state. Called from the confirmed-delete path so a
// lifetime of deleted workspaces cannot grow these maps without bound.
// Safe on a nil or already-pruned workspace — deletion is idempotent.
//
// Key agreement: pendingRelease, runSessionsSeen/Swept, and the
// coordinator's states are all keyed by scriptWorkspaceKey (identity sets
// for the run-session maps). If that keying ever changes, this function
// must change with it.
func (r *ScriptRunner) ForgetWorkspace(ws *data.Workspace) {
	if r == nil || ws == nil {
		return
	}
	key := scriptWorkspaceKey(ws)
	// Identity-set resolution does filesystem work — hoist it out of r.mu,
	// same convention as the key above.
	ids := data.WorkspaceIdentitySet(ws)
	var releaseRoot string
	r.mu.Lock()
	prefix := key + "|"
	for k := range r.lastOutput {
		if strings.HasPrefix(k, prefix) {
			delete(r.lastOutput, k)
		}
	}
	// An in-flight setup's own defer unregisters its tail; clearing here too
	// keeps a forgotten workspace from serving a live view it no longer owns.
	delete(r.liveSetup, key)
	for _, id := range ids {
		delete(r.runSessionsSeen, string(id))
		delete(r.runSessionsSwept, string(id))
	}
	// A parked nil-running release is dropped here — the sweep that would
	// drain it only runs inside IsRunning, which a deleted workspace never
	// reaches again; its port must still be returned. A pending release
	// pinned to a live running entry stays: the finish path drains it, and
	// deleting now would strand the reservation.
	if pending, ok := r.pendingRelease[key]; ok && pending.running == nil {
		delete(r.pendingRelease, key)
		releaseRoot = pending.root
	}
	r.mu.Unlock()
	if releaseRoot != "" && r.portAllocator != nil {
		r.portAllocator.ReleasePort(releaseRoot)
	}
	r.lifecycle.forget(key)
}

// missingScriptTypes lists the lifecycle types absent from the set — the
// fallback's trigger, so a workspace with only a setup transcript in memory
// still picks up a persisted archive tail.
func missingScriptTypes(have map[ScriptType]ScriptOutput) []ScriptType {
	var missing []ScriptType
	for _, st := range []ScriptType{ScriptSetup, ScriptArchive, ScriptOnDone} {
		if _, ok := have[st]; !ok {
			missing = append(missing, st)
		}
	}
	return missing
}

// SetScriptExitListener installs the callback for asynchronous lifecycle
// script exits — today only on-done, the one lifecycle hook whose Wait runs
// detached inside the runner (setup/archive return their outcome to a
// synchronous caller; `run` is reported through RunScriptStatus). Called
// with a non-nil runErr on non-zero exit, after the transcript is recorded,
// so the listener can point at LastScriptOutputs. Nil clears.
func (r *ScriptRunner) SetScriptExitListener(fn func(ws *data.Workspace, scriptType ScriptType, runErr error)) {
	r.mu.Lock()
	r.exitListener = fn
	r.mu.Unlock()
}

// notifyScriptExit invokes the listener outside the lock (a callback into
// the app pump must never run under r.mu — a re-entrant ScriptRunner call
// would deadlock).
func (r *ScriptRunner) notifyScriptExit(ws *data.Workspace, scriptType ScriptType, runErr error) {
	r.mu.Lock()
	fn := r.exitListener
	r.mu.Unlock()
	if fn != nil {
		fn(ws, scriptType, runErr)
	}
}
