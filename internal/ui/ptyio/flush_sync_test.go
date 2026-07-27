package ptyio

// Coverage for the DEC 2026 half of the flush policy: which prefix of the
// buffer is safe to hand the terminal, and what the chunk take does with the
// partial frame left at the tail. The flush-gate side lives with the rest of
// the timing policy in TestFlushDelay.

import (
	"bytes"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestScanSyncFrames(t *testing.T) {
	tests := []struct {
		name         string
		pending      []byte
		want         int
		wantComplete bool
	}{
		{"empty buffer", nil, 0, false},
		{"no markers", []byte("plain output"), len("plain output"), false},
		{"lone sync begin", []byte("\x1b[?2026h"), 0, false},
		{"open region with partial frame", []byte("\x1b[?2026h\x1b[2Khalf"), 0, false},
		{"complete region", []byte("\x1b[?2026hframe\x1b[?2026l"), len("\x1b[?2026hframe\x1b[?2026l"), true},
		{"trailing text after close", []byte("\x1b[?2026hframe\x1b[?2026lmore"), len("\x1b[?2026hframe\x1b[?2026lmore"), true},
		{"close without open", []byte("frame\x1b[?2026l"), len("frame\x1b[?2026l"), true},
		// The vterm answers DECRQM for mode 2026 (see executeDECRQM), so this
		// query really does appear on the wire. It is not a region boundary.
		{"DECRQM mode query", []byte("\x1b[?2026$p"), len("\x1b[?2026$p"), false},
		{
			"DECRQM query inside an open region",
			[]byte("\x1b[?2026h\x1b[?2026$p"),
			0, false,
		},
		{"marker truncated at the buffer tail", []byte("frame\x1b[?2026"), len("frame\x1b[?2026"), false},
		{
			"open region then a truncated marker",
			[]byte("\x1b[?2026hframe\x1b[?2026"),
			0, false,
		},
		{
			"complete then reopened stops at the boundary",
			[]byte("\x1b[?2026hone\x1b[?2026l\x1b[?2026htwo"),
			len("\x1b[?2026hone\x1b[?2026l"), true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, complete := scanSyncFrames(tt.pending)
			if got != tt.want {
				t.Fatalf("scanSyncFrames safeLen = %d, want %d", got, tt.want)
			}
			if complete != tt.wantComplete {
				t.Fatalf("scanSyncFrames hasCompleteFrame = %v, want %v", complete, tt.wantComplete)
			}
		})
	}

	// Only the tail is scanned, so a region that opened further back than
	// syncTailScanLimit reads as closed rather than holding the flush forever.
	t.Run("region older than the tail scan limit reads as closed", func(t *testing.T) {
		pending := append([]byte("\x1b[?2026h"), bytes.Repeat([]byte("x"), syncTailScanLimit+16)...)
		if got, _ := scanSyncFrames(pending); got != len(pending) {
			t.Fatalf("scanSyncFrames safeLen = %d, want %d (whole buffer) beyond the tail scan limit", got, len(pending))
		}
	})

	// A boundary inside the scanned tail is still reported as an absolute
	// offset into the full buffer, not an offset into the window.
	t.Run("boundary offset is absolute when the buffer is truncated", func(t *testing.T) {
		head := bytes.Repeat([]byte("x"), syncTailScanLimit)
		pending := append(append(head, []byte("\x1b[?2026hframe\x1b[?2026l")...), []byte("\x1b[?2026h")...)
		want := len(pending) - len("\x1b[?2026h")
		if got, _ := scanSyncFrames(pending); got != want {
			t.Fatalf("scanSyncFrames safeLen = %d, want %d", got, want)
		}
	})
}

func TestTakeFlushChunkLockedSyncBoundary(t *testing.T) {
	t.Run("stops at the last completed frame", func(t *testing.T) {
		complete := "\x1b[?2026hone\x1b[?2026l"
		st := &State{PendingOutput: []byte(complete + "\x1b[?2026h")}
		chunk := st.TakeFlushChunkLocked(0)
		if string(chunk) != complete {
			t.Fatalf("chunk = %q, want %q (complete frame only)", chunk, complete)
		}
		if string(st.PendingOutput) != "\x1b[?2026h" {
			t.Fatalf("PendingOutput = %q, want the partial frame left buffered", st.PendingOutput)
		}
	})

	t.Run("takes a buffer that is only a partial frame rather than stalling", func(t *testing.T) {
		st := &State{PendingOutput: []byte("\x1b[?2026h\x1b[2Khalf")}
		chunk := st.TakeFlushChunkLocked(0)
		if string(chunk) != "\x1b[?2026h\x1b[2Khalf" {
			t.Fatalf("chunk = %q, want the whole buffer (no safe boundary to clamp to)", chunk)
		}
		if len(st.PendingOutput) != 0 {
			t.Fatalf("PendingOutput = %q, want drained", st.PendingOutput)
		}
	})

	t.Run("maxChunk still wins when it is the tighter bound", func(t *testing.T) {
		st := &State{PendingOutput: []byte("\x1b[?2026hone\x1b[?2026l\x1b[?2026h")}
		chunk := st.TakeFlushChunkLocked(4)
		if len(chunk) != 4 {
			t.Fatalf("len(chunk) = %d, want 4 (maxChunk)", len(chunk))
		}
	})

	// When maxChunk cuts a large backlog the cut, not the buffer tail, is what
	// can land mid-frame — so the boundary just before the cut is the one to
	// find. The property under test is the terminal's state after the write,
	// which is what a mid-frame chunk actually damages.
	t.Run("a maxChunk cut inside a frame body falls back to the previous boundary", func(t *testing.T) {
		first := "\x1b[?2026hone\x1b[?2026l"
		st := &State{PendingOutput: []byte(first + "\x1b[?2026htwo\x1b[?2026l\x1b[?2026hthree\x1b[?2026l")}
		cut := len(first) + len("\x1b[?2026h") + 1 // inside the second frame's body
		chunk := st.TakeFlushChunkLocked(cut)
		if string(chunk) != first {
			t.Fatalf("chunk = %q, want %q (boundary before the cut)", chunk, first)
		}
		term := vterm.New(80, 24)
		term.Write(chunk)
		if term.SyncActive() {
			t.Fatal("chunk left the terminal mid-frame")
		}
	})

	// A cut inside the marker itself is harmless: no region has opened yet and
	// the vterm parser carries the fragment to the next chunk.
	t.Run("a maxChunk cut inside a marker leaves the terminal outside a frame", func(t *testing.T) {
		first := "\x1b[?2026hone\x1b[?2026l"
		st := &State{PendingOutput: []byte(first + "\x1b[?2026htwo\x1b[?2026l")}
		chunk := st.TakeFlushChunkLocked(len(first) + 5)
		term := vterm.New(80, 24)
		term.Write(chunk)
		if term.SyncActive() {
			t.Fatalf("chunk %q left the terminal mid-frame", chunk)
		}
	})

	t.Run("a maxChunk cut with no boundary before it takes the full chunk", func(t *testing.T) {
		st := &State{PendingOutput: []byte("\x1b[?2026h" + string(bytes.Repeat([]byte("x"), 200)) + "\x1b[?2026l")}
		chunk := st.TakeFlushChunkLocked(64)
		if len(chunk) != 64 {
			t.Fatalf("len(chunk) = %d, want 64 — no boundary exists to clamp to", len(chunk))
		}
	})

	t.Run("buffer without markers is unaffected", func(t *testing.T) {
		st := &State{PendingOutput: []byte("plain output")}
		chunk := st.TakeFlushChunkLocked(0)
		if string(chunk) != "plain output" {
			t.Fatalf("chunk = %q, want the whole buffer", chunk)
		}
	})
}

// BenchmarkScanSyncFrames guards the flush hot path: scanSyncFrames runs on
// the UI goroutine twice per flush (the gate, then the chunk take), so it has to
// stay negligible against a busy tab's buffer. The scan direction is the whole
// game here — an equivalent bytes.LastIndex implementation measured ~100x worse
// per byte, enough to cost more than the flush it guards.
func BenchmarkScanSyncFrames(b *testing.B) {
	frame := append(append([]byte("\x1b[?2026h"), bytes.Repeat([]byte("x"), 1400)...), []byte("\x1b[?2026l")...)
	cases := []struct {
		name    string
		pending []byte
	}{
		{"one frame", frame},
		{"no markers", bytes.Repeat([]byte("x"), 1400)},
		{"backlog past the scan limit", bytes.Repeat([]byte("x"), 1024*1024)},
		{"scan window packed with frames", bytes.Repeat(frame, syncTailScanLimit/len(frame)+1)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = scanSyncFrames(tc.pending)
			}
		})
	}
}

// TestFlushPipelineKeepsFramesWholeAndLossless drives the three pieces together
// — buffer, gate, chunk take, vterm write — over a stream shaped the way tmux
// actually delivers one: frames bracketed in DEC 2026, chopped into ≤1KB socket
// writes, so most reads land inside a region. It asserts the two properties the
// policy exists for: no flush ever leaves the terminal mid-frame, and not one
// byte is dropped or reordered on the way there.
//
// The ceilings are set out of reach and the quiet period to zero so the gate
// exercises the frame-boundary logic alone; the ceiling escapes are covered in
// TestFlushDelay.
func TestFlushPipelineKeepsFramesWholeAndLossless(t *testing.T) {
	// Two regimes. At a realistic cap no frame comes close to it, so the
	// no-mid-frame property must hold outright. At a cap smaller than a single
	// frame the split is forced, and the guarantee weakens to what is actually
	// achievable: the gap closes promptly and no byte is lost.
	t.Run("realistic chunk cap", func(t *testing.T) {
		flushPipelineRoundTrip(t, PtyFlushChunkSize, true)
	})
	t.Run("cap smaller than one frame", func(t *testing.T) {
		flushPipelineRoundTrip(t, 512, false)
	})
}

func flushPipelineRoundTrip(t *testing.T, chunkCap int, wantNoGaps bool) {
	t.Helper()
	const (
		tmuxWrite = 1024
		frames    = 40
		quiet     = time.Duration(0)
		ceiling   = time.Hour
	)

	var stream []byte
	maxFrame := 0
	for i := 0; i < frames; i++ {
		row := bytes.Repeat([]byte("x"), 700+37*i%900)
		frame := append([]byte("\x1b[?2026h"), row...)
		frame = append(frame, []byte("\x1b[?2026l")...)
		if len(frame) > maxFrame {
			maxFrame = len(frame)
		}
		stream = append(stream, frame...)
	}

	var reads [][]byte
	for off := 0; off < len(stream); off += tmuxWrite {
		end := off + tmuxWrite
		if end > len(stream) {
			end = len(stream)
		}
		reads = append(reads, stream[off:end])
	}

	// Sanity: the fixture has to reproduce the hazard, or the test proves nothing.
	midRegion, depth := 0, 0
	for _, r := range reads {
		depth += bytes.Count(r, []byte("\x1b[?2026h")) - bytes.Count(r, []byte("\x1b[?2026l"))
		if depth > 0 {
			midRegion++
		}
	}
	if midRegion == 0 {
		t.Fatalf("fixture does not split any region across reads (%d reads)", len(reads))
	}
	t.Logf("fixture: %d reads, %d (%d%%) end inside an open region",
		len(reads), midRegion, 100*midRegion/len(reads))

	term := vterm.New(120, 40)
	st := &State{}
	var written []byte
	now := time.Now()
	flushes, forced := 0, 0
	gapOpenedAt, maxGapFlushes := 0, 0

	for _, r := range reads {
		st.PendingOutput = append(st.PendingOutput, r...)
		now = now.Add(time.Millisecond)
		st.LastOutputAt = now
		if st.FlushPendingSince.IsZero() {
			st.FlushPendingSince = now
		}
		for {
			if _, deferred := st.FlushGate(now, quiet, ceiling); deferred {
				break
			}
			chunk := st.TakeFlushChunkLocked(chunkCap)
			if len(chunk) == 0 {
				t.Fatal("flush gate passed but the take returned nothing — the flush tick would spin")
			}
			flushes++
			written = append(written, chunk...)
			safe, _ := scanSyncFrames(chunk)
			wasActive := term.SyncActive()
			term.Write(chunk)
			// Check the transition, not the state: once the cap has forced a
			// split the terminal stays mid-frame until that frame's closing
			// bytes are written, and the chunks in between cannot help that.
			if !wasActive && term.SyncActive() {
				if wantNoGaps {
					t.Fatalf("flush %d opened a mid-frame gap (chunk %d bytes, cap %d, safe %d)",
						flushes, len(chunk), chunkCap, safe)
				}
				forced++
				gapOpenedAt = flushes
			}
			if wasActive && !term.SyncActive() && gapOpenedAt > 0 {
				if span := flushes - gapOpenedAt; span > maxGapFlushes {
					maxGapFlushes = span
				}
				gapOpenedAt = 0
			}
			if !st.RearmFlush(now, nil) {
				break
			}
		}
	}

	if flushes == 0 {
		t.Fatal("no flushes happened")
	}
	t.Logf("%d flushes, %d forced mid-frame gaps (widest %d flushes) at a %d-byte cap",
		flushes, forced, maxGapFlushes, chunkCap)
	if gapOpenedAt != 0 {
		t.Fatal("a mid-frame gap was still open when the stream ended")
	}
	// A forced split resolves as fast as the cap allows: the rest of the frame is
	// already buffered, so it takes the chunks that frame needs and no more.
	if want := (maxFrame+chunkCap-1)/chunkCap - 1; maxGapFlushes > want {
		t.Fatalf("a mid-frame gap stayed open for %d flushes, want at most %d "+
			"(largest frame %d bytes at a %d-byte cap)", maxGapFlushes, want, maxFrame, chunkCap)
	}
	if len(st.PendingOutput) != 0 {
		t.Fatalf("%d bytes still buffered after a stream ending on a frame boundary", len(st.PendingOutput))
	}
	if !bytes.Equal(written, stream) {
		t.Fatalf("stream corrupted: wrote %d bytes, want %d", len(written), len(stream))
	}
}

// The flush path deliberately ends a chunk right after ESC[?2026l, which puts
// the closing marker in the trailing line the noise filter inspects. That test
// runs on ANSI-stripped text, so a redraw whose visible tail happens to read
// like a macOS malloc diagnostic used to take the marker into the carry with it
// — leaving the terminal frozen on the previous frame despite the flush having
// written what it believed was a whole frame.
func TestWriteFilteredChunkKeepsSyncEnd(t *testing.T) {
	frame := "\x1b[?2026h\x1b[2Kredraw\r\nsomefunc(42)\x1b[?2026l"
	st := &State{PendingOutput: []byte(frame)}
	term := vterm.New(80, 24)

	chunk := st.TakeFlushChunkLocked(0)
	if safe, complete := scanSyncFrames(chunk); !complete || safe != len(chunk) {
		t.Fatalf("scan says safe=%d complete=%v, want the whole frame", safe, complete)
	}

	st.WriteFilteredChunkLocked(term.Write, chunk)

	if term.SyncActive() {
		t.Fatalf("terminal left sync-frozen; noise filter withheld %q", st.NoiseTrailing)
	}
	if bytes.Contains(st.NoiseTrailing, syncMarkerPrefix) {
		t.Fatalf("NoiseTrailing carries a sync marker: %q", st.NoiseTrailing)
	}
}

// The scan resumes at a non-terminator byte rather than past it, so a marker
// starting immediately after an aborted prefix is still seen. Malformed output
// only, but the vterm's parser cancels the aborted sequence and honors the
// second marker, and the scan has to agree with it.
func TestScanSyncFramesMarkerAfterAbortedPrefix(t *testing.T) {
	pending := []byte("\x1b[?2026hbody\x1b[?2026l\x1b[?2026\x1b[?2026hpartial")
	safe, complete := scanSyncFrames(pending)
	if want := len("\x1b[?2026hbody\x1b[?2026l"); safe != want {
		t.Fatalf("safeLen = %d, want %d (stop at the completed frame)", safe, want)
	}
	if !complete {
		t.Fatal("hasCompleteFrame = false, want true")
	}

	term := vterm.New(80, 24)
	term.Write(pending[:safe])
	if term.SyncActive() {
		t.Fatal("terminal left mid-frame after writing the reported safe prefix")
	}
}
