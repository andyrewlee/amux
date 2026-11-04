package common

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPickerLoadMachineCheck runs the exhaustive verifier over the
// directory-load lifecycle — issue only from dirty, clean only via a
// current-path result, stale never yields clean, Show always re-arms.
func TestPickerLoadMachineCheck(t *testing.T) {
	if err := PickerLoadCheck(); err != nil {
		t.Fatalf("picker load machine failed verification: %v", err)
	}
}

// TestFilePickerFlagsWalkLoadTable drives the real picker (real temp dirs,
// real async round-trips) through the table's lanes — the happy cycle, the
// stale lane, and the Show reset — asserting the (needsLoad, loadInFlight)
// pair never leaves a state the table permits. expect predicts the table's
// next state for an event; each action then asserts the flags landed there.
func TestFilePickerFlagsWalkLoadTable(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	state := func() pickerLoadState { return pickerStateFor(fp.needsLoad, fp.loadInFlight) }
	expect := func(evt pickerLoadEvent) pickerLoadState {
		t.Helper()
		next, ok := pickerLoadMachine.Next(state(), evt)
		if !ok {
			t.Fatalf("table rejected %v from %v (flags: needsLoad=%v inFlight=%v)",
				evt, state(), fp.needsLoad, fp.loadInFlight)
		}
		return next
	}
	assertAt := func(want pickerLoadState, what string) {
		t.Helper()
		if got := state(); got != want {
			t.Fatalf("%s: flags project %v, table predicts %v (needsLoad=%v inFlight=%v)",
				what, got, want, fp.needsLoad, fp.loadInFlight)
		}
	}

	// Construction marks the first load; Show resets the in-flight flag
	// (nothing issued yet) and re-arms the reload.
	want := expect(evtPickerShowReset)
	fp.Show()
	assertAt(want, "after Show")

	// The fallback issue path: Update delivers the due read off-goroutine
	// (the wheel is an arbitrary message that reaches the wrapper).
	want = expect(evtPickerIssueLoad)
	_, cmd := fp.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cmd == nil {
		t.Fatal("expected Update to issue the due directory load")
	}
	assertAt(want, "after issue")

	msgs := pumpMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one directoryLoadedMsg, got %d", len(msgs))
	}
	want = expect(evtPickerFreshLoad)
	fp.Update(msgs[0])
	assertAt(want, "after fresh result")

	// The stale lane: ctrl+h marks and re-issues (mark, then issue);
	// navigating while the read is out re-arms the debt without a second
	// read (in-flight self-loop); the superseded result then lands on
	// dirty — never clean.
	want = expect(evtPickerMarkLoad)
	want = func() pickerLoadState {
		next, ok := pickerLoadMachine.Next(want, evtPickerIssueLoad)
		if !ok {
			t.Fatalf("table rejected issue-load from %v", want)
		}
		return next
	}()
	_, cmd = fp.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected the hidden toggle to issue a reload")
	}
	assertAt(want, "after ctrl+h")

	want = expect(evtPickerMarkLoad)
	fp.markNeedsLoad()
	assertAt(want, "after navigate-while-in-flight")

	// Deliver the superseded result through the inner handler (the Update
	// wrapper would immediately re-issue the reload, muddying the check).
	want = expect(evtPickerStaleLoad)
	fp.update(directoryLoadedMsg{path: filepath.Join(tmp, "elsewhere")})
	assertAt(want, "after stale result (stale never yields clean)")

	pumpPicker(fp)
	assertAt(pickerClean, "after reload completes")

	// Show mid-flight resets the in-flight flag and re-arms the reload.
	_, cmd = fp.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected the hidden toggle to issue a reload")
	}
	assertAt(pickerInFlight, "after second ctrl+h")
	want = expect(evtPickerShowReset)
	fp.Hide()
	fp.Show()
	assertAt(want, "after Show mid-flight")
	pumpPicker(fp)
	assertAt(pickerClean, "after post-Show reload")
}
