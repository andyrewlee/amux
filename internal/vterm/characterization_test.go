package vterm

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// Characterization tests for the invariants the hardening refactors lean on:
// alt-screen capture must never lose or duplicate scrollback lines across
// arbitrary redraw sequences, UTF-8 runes must survive arbitrary chunk
// splits, and the render cache must never change what Render returns.

func plainLine(cells []Cell) string {
	var b strings.Builder
	for _, c := range cells {
		if c.Rune != 0 {
			b.WriteRune(c.Rune)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func scrollbackLines(v *VTerm) []string {
	out := make([]string, 0, len(v.Scrollback))
	for _, line := range v.Scrollback {
		out = append(out, plainLine(line))
	}
	return out
}

// TestAltScreenCaptureRedrawsNeverDuplicateFrames drives random clear-screen
// redraw cycles inside the alt screen and asserts the capture tracking
// neither duplicates a repeated frame in scrollback nor corrupts its
// bookkeeping fields.
func TestAltScreenCaptureRedrawsNeverDuplicateFrames(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(42))

	for trial := 0; trial < 25; trial++ {
		v := New(20, 5)
		v.AllowAltScreenScrollback = true
		for i := 0; i < 8; i++ {
			v.Write([]byte(fmt.Sprintf("hist %d\r\n", i)))
		}
		baseline := scrollbackLines(v)

		v.Write([]byte("\x1b[?1049h")) // enter alt screen

		frame := func(n int) []byte {
			var b strings.Builder
			b.WriteString("\x1b[2J\x1b[H")
			for row := 0; row < 3; row++ {
				fmt.Fprintf(&b, "\x1b[%d;1Hframe%d row%d", row+1, n, row)
			}
			return []byte(b.String())
		}

		// Redraw a random sequence of frames; consecutive identical frames are
		// the dedup case, distinct frames replace the tracked capture.
		for step := 0; step < 12; step++ {
			current := rng.Intn(3)
			v.Write(frame(current))

			// Invariants on the tracking fields after every redraw.
			if v.altScreenCaptureLen < 0 || v.altScreenCaptureDropLen < 0 || v.altScreenCaptureEndOffset < 0 {
				t.Fatalf("trial %d step %d: negative capture tracking: len=%d drop=%d end=%d",
					trial, step, v.altScreenCaptureLen, v.altScreenCaptureDropLen, v.altScreenCaptureEndOffset)
			}
			if v.altScreenCaptureLen+v.altScreenCaptureEndOffset > len(v.Scrollback) {
				t.Fatalf("trial %d step %d: capture tracking exceeds scrollback: len=%d end=%d scrollback=%d",
					trial, step, v.altScreenCaptureLen, v.altScreenCaptureEndOffset, len(v.Scrollback))
			}

			lines := scrollbackLines(v)
			// The original history must never be lost.
			for i, want := range baseline {
				if lines[i] != want {
					t.Fatalf("trial %d step %d: history line %d corrupted: got %q want %q", trial, step, i, lines[i], want)
				}
			}
			// The currently tracked frame must appear at most once in the
			// captured suffix (dedup invariant).
			count := 0
			marker := fmt.Sprintf("frame%d row0", current)
			for _, line := range lines[len(baseline):] {
				if line == marker {
					count++
				}
			}
			if count > 1 {
				t.Fatalf("trial %d step %d: frame %d captured %d times:\n%s",
					trial, step, current, count, strings.Join(lines, "\n"))
			}
		}

		v.Write([]byte("\x1b[?1049l")) // leave alt screen
		lines := scrollbackLines(v)
		for i, want := range baseline {
			if lines[i] != want {
				t.Fatalf("trial %d: history line %d corrupted after alt-screen exit: got %q want %q", trial, i, lines[i], want)
			}
		}
	}
}

// TestUTF8ContinuationAcrossArbitrarySplits writes multi-byte runes split at
// every possible chunk boundary and asserts the decoded screen content is
// identical to an unsplit write.
func TestUTF8ContinuationAcrossArbitrarySplits(t *testing.T) {
	t.Parallel()
	payload := []byte("héllo→世界🙂ok")

	reference := New(40, 3)
	reference.Write(payload)
	want := plainLine(reference.Screen[0])

	for split := 1; split < len(payload); split++ {
		v := New(40, 3)
		v.Write(payload[:split])
		v.Write(payload[split:])
		if got := plainLine(v.Screen[0]); got != want {
			t.Fatalf("split at %d: got %q want %q", split, got, want)
		}
	}

	// Byte-at-a-time is the most adversarial split.
	v := New(40, 3)
	for _, b := range payload {
		v.Write([]byte{b})
	}
	if got := plainLine(v.Screen[0]); got != want {
		t.Fatalf("byte-at-a-time: got %q want %q", got, want)
	}
}

// TestRenderCacheNeverChangesOutput interleaves random writes, scrolls and
// cursor moves, asserting after each step that the cached Render equals the
// render produced with the cache fully invalidated.
func TestRenderCacheNeverChangesOutput(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(7))
	v := New(30, 6)

	ops := []func(int){
		func(i int) { v.Write([]byte(fmt.Sprintf("line %d output\r\n", i))) },
		func(i int) { v.Write([]byte(fmt.Sprintf("\x1b[%d;%dHX", 1+rng.Intn(6), 1+rng.Intn(30)))) },
		func(i int) { v.Write([]byte("\x1b[31mred\x1b[0m")) },
		func(i int) { v.ScrollView(1) },
		func(i int) { v.ScrollViewToBottom() },
	}

	for step := 0; step < 200; step++ {
		ops[rng.Intn(len(ops))](step)

		cached := v.Render()
		v.ClearDirty()
		again := v.Render()
		if cached != again {
			t.Fatalf("step %d: second cached render differs from first", step)
		}
		v.invalidateRenderCache()
		fresh := v.Render()
		if cached != fresh {
			t.Fatalf("step %d: cached render differs from cache-invalidated render\ncached:\n%q\nfresh:\n%q", step, cached, fresh)
		}
	}
}
