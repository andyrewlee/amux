package process

import (
	"fmt"
	"strings"
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
type tailWriter struct {
	buf     []byte
	max     int
	dropped int
}

func (w *tailWriter) Write(p []byte) (int, error) {
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
	if w.dropped > 0 {
		return fmt.Sprintf("[... %d bytes of earlier output dropped ...]\n%s", w.dropped, w.buf)
	}
	return string(w.buf)
}

// ScriptOutput is the recorded tail of one lifecycle-script run — what the
// script emitted and how it finished. Err is the Wait error's text
// (empty on success). The JSON tags back the persisted transcript envelope
// (script_output_store.go); field names stay stable for the schema.
type ScriptOutput struct {
	Text       string    `json:"text"`
	Err        string    `json:"error,omitempty"`
	FinishedAt time.Time `json:"finished_at"`
}

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
	r.mu.Lock()
	r.lastOutput[scriptOutputKey(ws, scriptType)] = entry
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
	// Disk fallback fills types memory lacks — memory always wins because it
	// holds this process's freshest run. Loaded entries are folded back into
	// lastOutput so subsequent reads stay on the fast path.
	if missing := missingScriptTypes(out); len(missing) > 0 {
		for st, entry := range r.loadScriptOutputs(ws) {
			if _, have := out[st]; have {
				continue
			}
			out[st] = entry
			r.mu.Lock()
			r.lastOutput[scriptOutputKey(ws, st)] = entry
			r.mu.Unlock()
		}
	}
	return out
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
