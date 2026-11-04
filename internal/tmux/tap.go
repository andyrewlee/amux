package tmux

import (
	"io"
	"time"

	"github.com/andyrewlee/amux/internal/agentstatus"
)

// StatusTapArgv is the reserved helper invocation a pipe-pane install embeds
// (after the binary path): the tmux server name, the hosted session's
// name, and the install-time pane id. Baking all three at install time is
// what lets the helper run under the tmux server's environment — which
// carries neither $TMUX nor the amux config that DefaultOptions would
// otherwise resolve — and what keeps the write target pinned to the pane
// the pipe reads, even after the session's active pane changes.
const StatusTapInvocation = "--internal-status-tap"

// statusTap is the per-stream state of one tap helper: the protocol record
// set (one record per id, folded to one presentation view) plus the OSC 133
// prompt detector that drives the protocol's record lifetime.
type statusTap struct {
	records *agentstatus.RecordSet
	scanner *agentstatus.Scanner
	prompts *agentstatus.PromptDetector
}

func newStatusTap() *statusTap {
	return &statusTap{
		records: agentstatus.NewRecordSet(),
		scanner: &agentstatus.Scanner{},
		prompts: &agentstatus.PromptDetector{},
	}
}

// feed processes one chunk of pane output and reports whether the stored
// presentation view changed (the caller re-publishes when it did).
func (t *statusTap) feed(p []byte) bool {
	changed := false
	// A new shell prompt ends the previous command's working/blocked
	// records (protocol lifetime), so run the detector first: the prompt
	// mark typically precedes the reports of whatever runs next.
	if t.prompts.Feed(p) {
		changed = t.records.DropTransient() || changed
	}
	for _, payload := range t.scanner.Feed(p) {
		if report, ok := agentstatus.ParsePayload(payload); ok {
			changed = t.records.Apply(report) || changed
		}
	}
	return changed
}

// report renders the folded presentation view — the record amux surfaces
// for the pane, stamped now. An empty State means no records remain.
func (t *statusTap) report(nowMS int64) agentstatus.Report {
	rec, ok := t.records.Fold()
	if !ok {
		return agentstatus.Report{}
	}
	return agentstatus.Report{
		State:   rec.State,
		ID:      rec.ID,
		App:     rec.App,
		Kind:    rec.Kind,
		Message: rec.Message,
		AtMS:    nowMS,
	}
}

// RunStatusTap is the pipe-pane helper: read a pane's raw output forever,
// extract OSC 7501 reports per the Program Status Protocol, hold the
// protocol record set (replace-on-report, clear, prompt/exit lifetime),
// and publish the folded view as the pane's options. paneID is the
// install-time pane id the pipe reads (a "%N" token); reports are written
// to that exact pane, never to the session's current active pane, so a
// split after install cannot stamp one pane's report onto another. An
// empty paneID falls back to the session target — the pre-id install
// shape, kept so a stale helper argv still hosts.
//
// It lives here (not in cmd/amux) so the test binary can stand in for the
// amux launcher exactly as it does for the pane-launch helper, and so a
// future non-TUI host embeds the same loop.
//
// The tap never writes to its own stdout (pipe-pane -O guarantees stdout
// is discarded, but staying silent keeps a future flag change harmless)
// and never exits on one bad report — the next one is a full recovery.
// A write failure (a report the server rejected) is dropped the same way:
// the tap's job is observation, and killing it would lose every later
// report to salvage one. EOF (the pane's output ended — under
// remain-on-exit, the pane's process died) ends working/blocked records
// before the final publish; done and error survive, so a program that
// reports done right before exiting leaves its record behind.
func RunStatusTap(r io.Reader, serverName, sessionName, paneID string) int {
	opts := DefaultOptions()
	opts.ServerName = serverName
	writeTarget := paneID
	if writeTarget == "" {
		writeTarget = sessionName
	}
	tap := newStatusTap()
	publish := func() {
		report := tap.report(time.Now().UnixMilli())
		_ = WritePaneReport(writeTarget, report, opts)
	}
	buf := make([]byte, 64*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 && tap.feed(buf[:n]) {
			publish()
		}
		if err == io.EOF {
			if tap.records.DropTransient() {
				publish()
			}
			return 0
		}
		if err != nil {
			return 1
		}
	}
}

// HandleStatusTapInvocation dispatches the reserved tap argv for helper-mode
// binaries. It mirrors panelaunch.HandleInvocation's contract: handled=false
// for any other argv, a handled usage failure (exit 2) for a malformed
// reserved invocation, so a stray helper argv can never fall through into a
// host program's normal startup.
func HandleStatusTapInvocation(args []string, stdin io.Reader) (handled bool, exitCode int) {
	if len(args) == 0 || args[0] != StatusTapInvocation {
		return false, 0
	}
	if len(args) != 4 {
		return true, 2
	}
	return true, RunStatusTap(stdin, args[1], args[2], args[3])
}
