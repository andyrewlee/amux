package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

// restoreAttachSeams snapshots every attach/bootstrap seam var and restores it
// when the test ends. Tests then assign only the seams they care about instead
// of hand-rolling a save/restore block per seam. Because the seams are
// package-level, tests using this must not call t.Parallel.
func restoreAttachSeams(t *testing.T) {
	t.Helper()
	oldEnsureTmuxAvailable := ensureTmuxAvailableFn
	oldSessionStateFor := sessionStateForFn
	oldProbeSession := probeSessionFn
	oldNewPTYWithSize := newPTYWithSizeFn
	oldResizePaneToSize := resizePaneToSizeFn
	oldCapturePaneFullData := capturePaneFullDataFn
	oldCapturePaneHistoryData := capturePaneHistoryDataFn
	oldCapturePane := capturePaneFn
	oldVerifyTerminalSessionTags := verifyTerminalSessionTagsFn
	t.Cleanup(func() {
		ensureTmuxAvailableFn = oldEnsureTmuxAvailable
		sessionStateForFn = oldSessionStateFor
		probeSessionFn = oldProbeSession
		newPTYWithSizeFn = oldNewPTYWithSize
		resizePaneToSizeFn = oldResizePaneToSize
		capturePaneFullDataFn = oldCapturePaneFullData
		capturePaneHistoryDataFn = oldCapturePaneHistoryData
		capturePaneFn = oldCapturePane
		verifyTerminalSessionTagsFn = oldVerifyTerminalSessionTags
	})
}

// eligibleAttachProbe is a session probe that passes every bootstrap gate:
// detached, quiet, a single live pane, complete metadata. Tests start from it
// and change the one fact under test.
func eligibleAttachProbe() tmux.SessionProbe {
	return tmux.SessionProbe{
		Exists:           true,
		CreatedAt:        111,
		PaneID:           "%1",
		PaneCols:         123,
		PaneRows:         45,
		HasPaneSize:      true,
		HasLivePane:      true,
		SinglePaneWindow: true,
		PaneMeta: tmux.PaneSnapshotMeta{
			Cols:      123,
			Rows:      45,
			HasSize:   true,
			ModeState: tmux.PaneModeState{HasState: true},
		},
	}
}

// probeSeq installs a probeSessionFn handing out the given probes in order,
// repeating the last once exhausted, recording each call in calls. The bootstrap
// guards are a sequence of point-in-time reads, so scripting the probes is how a
// test says "the session changed at step N".
func probeSeq(calls *[]string, probes ...tmux.SessionProbe) {
	i := 0
	probeSessionFn = func(string, tmux.Options) (tmux.SessionProbe, error) {
		if calls != nil {
			*calls = append(*calls, "probe")
		}
		p := probes[i]
		if i < len(probes)-1 {
			i++
		}
		return p, nil
	}
}
