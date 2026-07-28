package vterm

import "testing"

// The alternate-screen modes each keep their own state. These tests pin the two
// properties that used to be conflated:
//
//  1. DECSC/DECRC (ESC 7 / ESC 8) is per-buffer. A full-screen application
//     saving the cursor must not clobber what the shell saved before it ran.
//  2. Modes 47, 1047 and 1049 are not interchangeable. Only 1049 saves and
//     restores the cursor across the buffer switch.
//
// TestAltScreen1049RoundTripIsUnchanged is the regression guard on the common
// path: 1049 is what nearly every application uses, so its behavior must be
// exactly what it always was.

func altVT(t *testing.T) *VTerm {
	t.Helper()
	return New(20, 10)
}

// cursorAt reports the cursor position as a comparable pair.
func cursorAt(v *VTerm) [2]int { return [2]int{v.CursorX, v.CursorY} }

// TestDECSCIsPerBuffer is the bug this fixes. Before, both buffers shared one
// saved-cursor slot, so a `vim`-style application issuing ESC 7 destroyed the
// position the shell had saved, and the shell's later ESC 8 jumped somewhere
// arbitrary.
func TestDECSCIsPerBuffer(t *testing.T) {
	vt := altVT(t)

	// The shell parks the cursor and saves it.
	vt.Write([]byte("\x1b[6;11H")) // row 6, col 11 -> (10, 5) zero-indexed
	vt.Write([]byte("\x1b7"))      // DECSC
	if got := cursorAt(vt); got != [2]int{10, 5} {
		t.Fatalf("setup: cursor = %v, want [10 5]", got)
	}

	// A full-screen application takes over and saves its own cursor.
	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("\x1b[3;4H")) // (3, 2)
	vt.Write([]byte("\x1b7"))     // the application's DECSC
	vt.Write([]byte("\x1b[1;1H"))
	vt.Write([]byte("\x1b8")) // its own DECRC still works inside the alt buffer
	if got := cursorAt(vt); got != [2]int{3, 2} {
		t.Fatalf("in-alt DECRC restored to %v, want the alt buffer's own save [3 2]", got)
	}

	// The application exits and the shell restores what *it* saved.
	vt.Write([]byte("\x1b[?1049l"))
	vt.Write([]byte("\x1b[1;1H"))
	vt.Write([]byte("\x1b8"))
	if got := cursorAt(vt); got != [2]int{10, 5} {
		t.Fatalf("primary DECRC restored to %v; the alt buffer's save clobbered it (want [10 5])", got)
	}
}

// TestDECSCStyleIsPerBuffer covers the other half of the DECSC slot: the saved
// graphic rendition travels with the position and must be per-buffer too.
func TestDECSCStyleIsPerBuffer(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[1m")) // bold
	vt.Write([]byte("\x1b7"))
	if !vt.SavedStyle.Bold {
		t.Fatal("setup: primary saved style is not bold")
	}

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("\x1b[0m")) // the application resets attributes
	vt.Write([]byte("\x1b7"))   // and saves its own plain style
	if vt.SavedStyle.Bold {
		t.Fatal("the alt buffer inherited the primary buffer's saved style")
	}

	vt.Write([]byte("\x1b[?1049l"))
	if !vt.SavedStyle.Bold {
		t.Fatal("the primary buffer's saved style was not restored on exit")
	}
}

// TestAltBufferStartsWithFreshSavedCursor asserts the alternate buffer does not
// inherit the primary's saved position — an application's first ESC 8 before
// any ESC 8 of its own must go home, not to wherever the shell was.
func TestAltBufferStartsWithFreshSavedCursor(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[8;15H"))
	vt.Write([]byte("\x1b7"))

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("\x1b[5;5H"))
	vt.Write([]byte("\x1b8")) // restore without ever having saved in this buffer
	if got := cursorAt(vt); got != [2]int{0, 0} {
		t.Fatalf("alt-buffer DECRC went to %v, want the origin [0 0]", got)
	}
}

// TestAltScreen1049RoundTripIsUnchanged pins the common path. Mode 1049 must
// still save the cursor on entry, home it in the alternate buffer, and put it
// back on exit — the behavior every full-screen application depends on.
func TestAltScreen1049RoundTripIsUnchanged(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[4;7H")) // (6, 3)
	before := cursorAt(vt)

	vt.Write([]byte("\x1b[?1049h"))
	if !vt.AltScreen {
		t.Fatal("1049h did not enter the alternate screen")
	}
	if got := cursorAt(vt); got != [2]int{0, 0} {
		t.Fatalf("1049h left the cursor at %v, want it homed at [0 0]", got)
	}

	vt.Write([]byte("\x1b[9;19H")) // move around inside the application

	vt.Write([]byte("\x1b[?1049l"))
	if vt.AltScreen {
		t.Fatal("1049l did not leave the alternate screen")
	}
	if got := cursorAt(vt); got != before {
		t.Fatalf("1049l restored the cursor to %v, want the pre-entry %v", got, before)
	}
}

// TestAltScreen1049HomesToTrueOriginUnderOriginMode guards a subtlety on the
// common path: with DECOM set and a scroll region that does not start at the
// top, 1049 must still home to row 0, not to the scroll region's top. Clamping
// the homed cursor would quietly move it.
func TestAltScreen1049HomesToTrueOriginUnderOriginMode(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[4;9r")) // scroll region rows 4..9
	vt.Write([]byte("\x1b[?6h"))  // DECOM on
	vt.Write([]byte("\x1b[2;3H"))

	vt.Write([]byte("\x1b[?1049h"))
	if got := cursorAt(vt); got != [2]int{0, 0} {
		t.Fatalf("1049h under origin mode homed to %v, want the true origin [0 0]", got)
	}
}

// TestAltScreen47DoesNotTouchTheCursor asserts mode 47 is the bare
// buffer switch a real terminal implements: no homing on entry, no restore on
// exit. Treating it like 1049 silently gave applications a guarantee the mode
// does not make.
func TestAltScreen47DoesNotTouchTheCursor(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[4;7H")) // (6, 3)
	entry := cursorAt(vt)

	vt.Write([]byte("\x1b[?47h"))
	if !vt.AltScreen {
		t.Fatal("47h did not enter the alternate screen")
	}
	if got := cursorAt(vt); got != entry {
		t.Fatalf("47h moved the cursor to %v; it must be left at %v", got, entry)
	}

	vt.Write([]byte("\x1b[2;3H")) // (2, 1)
	inAlt := cursorAt(vt)

	vt.Write([]byte("\x1b[?47l"))
	if vt.AltScreen {
		t.Fatal("47l did not leave the alternate screen")
	}
	if got := cursorAt(vt); got != inAlt {
		t.Fatalf("47l restored the cursor to %v; mode 47 must leave it at %v", got, inAlt)
	}
}

// TestAltScreen1047DoesNotRestoreTheCursor asserts 1047 shares 47's cursor
// semantics — it differs only in clearing the alternate screen on exit.
func TestAltScreen1047DoesNotRestoreTheCursor(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[4;7H"))
	vt.Write([]byte("\x1b[?1047h"))
	vt.Write([]byte("\x1b[2;3H"))
	inAlt := cursorAt(vt)

	vt.Write([]byte("\x1b[?1047l"))
	if got := cursorAt(vt); got != inAlt {
		t.Fatalf("1047l restored the cursor to %v; mode 1047 must leave it at %v", got, inAlt)
	}
}

// TestAltScreenReentryStartsBlank pins the property that makes 1047's
// exit-clear moot here: amux keeps no persistent alternate buffer, so
// re-entering the alternate screen always yields a blank one and a previous
// application's frame can never reappear. This holds for all three modes, which
// is why setAltScreenMode implements no separate clear step.
func TestAltScreenReentryStartsBlank(t *testing.T) {
	for _, mode := range []string{"47", "1047", "1049"} {
		t.Run(mode, func(t *testing.T) {
			vt := altVT(t)

			vt.Write([]byte("\x1b[?" + mode + "h"))
			vt.Write([]byte("\x1b[1;1HSECRET ALT FRAME"))
			if !screenContains(vt, "SECRET ALT FRAME") {
				t.Fatal("setup: the alt-screen text was not written")
			}
			vt.Write([]byte("\x1b[?" + mode + "l"))

			// Re-enter: the previous application's frame must be gone.
			vt.Write([]byte("\x1b[?" + mode + "h"))
			if screenContains(vt, "SECRET ALT FRAME") {
				t.Fatalf("mode %s showed the previous alternate frame on re-entry", mode)
			}
			for y := range vt.Screen {
				for x := range vt.Screen[y] {
					if r := vt.Screen[y][x].Rune; r != ' ' && r != 0 {
						t.Fatalf("mode %s re-entered onto a non-blank alt screen (row %d has %q)", mode, y, r)
					}
				}
			}
		})
	}
}

// TestAltScreenModesRestorePrimaryContents asserts the property common to all
// three modes: whatever was on the primary screen comes back.
func TestAltScreenModesRestorePrimaryContents(t *testing.T) {
	for _, mode := range []string{"47", "1047", "1049"} {
		t.Run(mode, func(t *testing.T) {
			vt := altVT(t)
			vt.Write([]byte("keep me"))

			vt.Write([]byte("\x1b[?" + mode + "h"))
			vt.Write([]byte("\x1b[1;1Halt content"))
			vt.Write([]byte("\x1b[?" + mode + "l"))

			if !screenContains(vt, "keep me") {
				t.Fatalf("mode %s did not restore the primary screen", mode)
			}
			if screenContains(vt, "alt content") {
				t.Fatalf("mode %s left alternate-screen content on the primary screen", mode)
			}
		})
	}
}

// TestUnbalancedAltScreenExitKeepsSavedCursor asserts the guard on the stash:
// an exit with no matching entry (a reattach can clear the alt flag out of
// band) must not install a zeroed saved cursor over a live one.
func TestUnbalancedAltScreenExitKeepsSavedCursor(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[7;9H"))
	vt.Write([]byte("\x1b7"))
	saved := [2]int{vt.SavedCursorX, vt.SavedCursorY}

	// Exit without ever entering.
	vt.Write([]byte("\x1b[?1049l"))
	if got := [2]int{vt.SavedCursorX, vt.SavedCursorY}; got != saved {
		t.Fatalf("an unpaired 1049l changed the saved cursor to %v, want %v", got, saved)
	}

	vt.Write([]byte("\x1b[1;1H"))
	vt.Write([]byte("\x1b8"))
	if got := cursorAt(vt); got != [2]int{8, 6} {
		t.Fatalf("DECRC after an unpaired exit went to %v, want [8 6]", got)
	}
}

// TestRepeatedAltScreenEntryDoesNotLoseThePrimarySave asserts the stash is not
// overwritten by a second entry while already in the alternate screen — the
// primary slot must survive to the eventual exit.
func TestRepeatedAltScreenEntryDoesNotLoseThePrimarySave(t *testing.T) {
	vt := altVT(t)

	vt.Write([]byte("\x1b[6;11H"))
	vt.Write([]byte("\x1b7"))

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("\x1b[2;2H"))
	vt.Write([]byte("\x1b7"))       // the application's own save
	vt.Write([]byte("\x1b[?1049h")) // a redundant re-entry
	vt.Write([]byte("\x1b[?1049l"))

	vt.Write([]byte("\x1b[1;1H"))
	vt.Write([]byte("\x1b8"))
	if got := cursorAt(vt); got != [2]int{10, 5} {
		t.Fatalf("after a redundant re-entry, primary DECRC went to %v, want [10 5]", got)
	}
}

// screenContains reports whether text appears anywhere on the visible screen.
func screenContains(v *VTerm, text string) bool {
	for y := range v.Screen {
		var row []rune
		for x := range v.Screen[y] {
			row = append(row, v.Screen[y][x].Rune)
		}
		if containsRunes(row, []rune(text)) {
			return true
		}
	}
	return false
}

func containsRunes(haystack, needle []rune) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
