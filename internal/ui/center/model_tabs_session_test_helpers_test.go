package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

func setKnownViewport(m *Model) {
	m.width = 120
	m.height = 40
}

// restoreReattachSeams snapshots every reattach/bootstrap seam var and restores
// it when the test ends. Tests then assign only the seams they care about
// instead of hand-rolling a save/restore block per seam. Because the seams are
// package-level, tests using this must not call t.Parallel.
func restoreReattachSeams(t *testing.T) {
	t.Helper()
	oldSessionStateFor := sessionStateForFn
	oldSessionOwned := sessionOwnedFn
	oldProbeSession := ptyio.ProbeSessionFn
	oldKillSession := killSessionFn
	oldResizePaneToSize := ptyio.ResizePaneToSizeFn
	oldCapturePaneFullData := ptyio.CapturePaneFullDataFn
	oldCapturePaneHistoryData := ptyio.CapturePaneHistoryDataFn
	oldCapturePane := capturePaneFn
	oldCreateAgentWithTags := createAgentWithTagsFn
	oldCreateRunAttach := createRunAttachFn
	// Ownership defaults to owned: the reattach tests exercise
	// dead/attachable session states, not the shared-server squatting check —
	// tests for that override sessionOwnedFn after this helper runs.
	sessionOwnedFn = func(string, []string, tmux.Options) (bool, error) { return true, nil }
	t.Cleanup(func() {
		sessionStateForFn = oldSessionStateFor
		sessionOwnedFn = oldSessionOwned
		ptyio.ProbeSessionFn = oldProbeSession
		killSessionFn = oldKillSession
		ptyio.ResizePaneToSizeFn = oldResizePaneToSize
		ptyio.CapturePaneFullDataFn = oldCapturePaneFullData
		ptyio.CapturePaneHistoryDataFn = oldCapturePaneHistoryData
		capturePaneFn = oldCapturePane
		createAgentWithTagsFn = oldCreateAgentWithTags
		createRunAttachFn = oldCreateRunAttach
	})
}

// eligibleReattachProbe is a session probe that passes every bootstrap gate:
// detached, quiet, a single live pane, complete metadata. Tests start from it
// and change the one fact under test.
func eligibleReattachProbe() tmux.SessionProbe {
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

// probeSeq installs a ptyio.ProbeSessionFn handing out the given probes in order,
// repeating the last once exhausted, recording each call in calls. The bootstrap
// guards are a sequence of point-in-time reads, so scripting the probes is how a
// test says "the session changed at step N".
func probeSeq(calls *[]string, probes ...tmux.SessionProbe) {
	i := 0
	ptyio.ProbeSessionFn = func(string, tmux.Options) (tmux.SessionProbe, error) {
		*calls = append(*calls, "probe")
		p := probes[i]
		if i < len(probes)-1 {
			i++
		}
		return p, nil
	}
}
