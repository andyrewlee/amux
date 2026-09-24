package ptyio

import (
	"bytes"
	"testing"
)

func TestTakeFlushChunkLocked(t *testing.T) {
	t.Run("nil buffer returns nil", func(t *testing.T) {
		st := &State{}
		if got := st.TakeFlushChunkLocked(16); got != nil {
			t.Fatalf("got %q, want nil for empty buffer", got)
		}
	})

	t.Run("takes up to maxChunk bytes and advances the buffer", func(t *testing.T) {
		st := &State{PendingOutput: []byte("ABCDEFGHIJ")}
		got := st.TakeFlushChunkLocked(4)
		if string(got) != "ABCD" {
			t.Fatalf("chunk = %q, want %q", got, "ABCD")
		}
		if string(st.PendingOutput) != "EFGHIJ" {
			t.Fatalf("remaining = %q, want %q", st.PendingOutput, "EFGHIJ")
		}
	})

	t.Run("non-positive maxChunk takes the whole buffer", func(t *testing.T) {
		st := &State{PendingOutput: []byte("ABCDEF")}
		got := st.TakeFlushChunkLocked(0)
		if string(got) != "ABCDEF" {
			t.Fatalf("chunk = %q, want %q", got, "ABCDEF")
		}
		if len(st.PendingOutput) != 0 {
			t.Fatalf("remaining = %q, want empty", st.PendingOutput)
		}

		st = &State{PendingOutput: []byte("XYZ")}
		got = st.TakeFlushChunkLocked(-1)
		if string(got) != "XYZ" {
			t.Fatalf("chunk = %q, want %q for negative maxChunk", got, "XYZ")
		}
		if len(st.PendingOutput) != 0 {
			t.Fatalf("remaining = %q, want empty", st.PendingOutput)
		}
	})

	t.Run("maxChunk larger than buffer returns the whole buffer", func(t *testing.T) {
		st := &State{PendingOutput: []byte("hi")}
		got := st.TakeFlushChunkLocked(100)
		if string(got) != "hi" {
			t.Fatalf("chunk = %q, want %q", got, "hi")
		}
		if len(st.PendingOutput) != 0 {
			t.Fatalf("remaining = %q, want empty", st.PendingOutput)
		}
	})

	t.Run("maxChunk equal to buffer length returns whole buffer", func(t *testing.T) {
		st := &State{PendingOutput: []byte("abcd")}
		got := st.TakeFlushChunkLocked(4)
		if string(got) != "abcd" {
			t.Fatalf("chunk = %q, want %q", got, "abcd")
		}
		if len(st.PendingOutput) != 0 {
			t.Fatalf("remaining = %q, want empty", st.PendingOutput)
		}
	})

	t.Run("returned chunk is a copy that does not alias PendingOutput", func(t *testing.T) {
		st := &State{PendingOutput: []byte("ABCDEFGH")}
		got := st.TakeFlushChunkLocked(4)
		if string(got) != "ABCD" {
			t.Fatalf("chunk = %q, want %q", got, "ABCD")
		}
		// Subsequent take shifts remaining bytes into the underlying array; the
		// earlier copy must be unaffected.
		_ = st.TakeFlushChunkLocked(4)
		if string(got) != "ABCD" {
			t.Fatalf("first chunk mutated by later take: got %q", got)
		}
	})

	t.Run("repeated takes drain the buffer in order", func(t *testing.T) {
		st := &State{PendingOutput: []byte("123456")}
		var assembled []byte
		for {
			chunk := st.TakeFlushChunkLocked(2)
			if chunk == nil {
				break
			}
			assembled = append(assembled, chunk...)
		}
		if string(assembled) != "123456" {
			t.Fatalf("assembled = %q, want %q", assembled, "123456")
		}
		if len(st.PendingOutput) != 0 {
			t.Fatalf("remaining = %q, want empty", st.PendingOutput)
		}
	})
}

func TestWriteFilteredChunkLocked(t *testing.T) {
	t.Run("writes visible bytes through and returns filtered output", func(t *testing.T) {
		st := &State{}
		var written []byte
		write := func(b []byte) { written = append(written, b...) }

		chunk := []byte("hello world\n")
		got := st.WriteFilteredChunkLocked(write, chunk)
		if string(got) != "hello world\n" {
			t.Fatalf("filtered = %q, want %q", got, "hello world\n")
		}
		if string(written) != "hello world\n" {
			t.Fatalf("written = %q, want %q", written, "hello world\n")
		}
	})

	t.Run("strips a known malloc diagnostic line and does not write it", func(t *testing.T) {
		st := &State{}
		var written []byte
		write := func(b []byte) { written = append(written, b...) }

		chunk := []byte("hi\r\ncodex(32758,0x16f58f000) malloc: nano zone abandoned\r\nbye\r\n")
		got := st.WriteFilteredChunkLocked(write, chunk)
		want := "hi\r\nbye\r\n"
		if string(got) != want {
			t.Fatalf("filtered = %q, want %q", got, want)
		}
		if string(written) != want {
			t.Fatalf("written = %q, want %q", written, want)
		}
	})

	t.Run("fully-filtered chunk does not invoke write", func(t *testing.T) {
		st := &State{}
		writeCalled := false
		write := func([]byte) { writeCalled = true }

		chunk := []byte("codex(32758) malloc: debugging enabled\n")
		got := st.WriteFilteredChunkLocked(write, chunk)
		if len(got) != 0 {
			t.Fatalf("filtered = %q, want empty", got)
		}
		if writeCalled {
			t.Fatalf("write was called for a fully-filtered chunk")
		}
	})

	t.Run("empty chunk returns empty and does not write", func(t *testing.T) {
		st := &State{}
		writeCalled := false
		write := func([]byte) { writeCalled = true }

		got := st.WriteFilteredChunkLocked(write, nil)
		if len(got) != 0 {
			t.Fatalf("filtered = %q, want empty", got)
		}
		if writeCalled {
			t.Fatalf("write was called for an empty chunk")
		}
	})

	t.Run("carries an incomplete diagnostic fragment across chunks via NoiseTrailing", func(t *testing.T) {
		st := &State{}
		var written []byte
		write := func(b []byte) { written = append(written, b...) }

		// First chunk ends mid-diagnostic: the fragment is held in NoiseTrailing.
		got1 := st.WriteFilteredChunkLocked(write, []byte("ok\nagent(32758) malloc: nano"))
		if string(got1) != "ok\n" {
			t.Fatalf("first filtered = %q, want %q", got1, "ok\n")
		}
		if len(st.NoiseTrailing) == 0 {
			t.Fatalf("expected NoiseTrailing to buffer the split diagnostic fragment")
		}

		// Second chunk completes the diagnostic; the whole line is suppressed.
		got2 := st.WriteFilteredChunkLocked(write, []byte(" zone abandoned\ndone\n"))
		if string(got2) != "done\n" {
			t.Fatalf("second filtered = %q, want %q", got2, "done\n")
		}
		if len(st.NoiseTrailing) != 0 {
			t.Fatalf("expected NoiseTrailing to be consumed, got %q", st.NoiseTrailing)
		}
		if string(written) != "ok\ndone\n" {
			t.Fatalf("written = %q, want %q", written, "ok\ndone\n")
		}
	})
}

// TestFlushNoiseTrailingLocked pins the flush-boundary release: a
// `name(N)`-shaped tail held by the noise filter is released when the flush
// drains — a no-newline prompt like "Retry(2)" must render without waiting
// for the next chunk or a reader drain.
func TestFlushNoiseTrailingLocked(t *testing.T) {
	st := &State{}
	var written []byte
	write := func(b []byte) { written = append(written, b...) }

	// A bare prompt with no newline gets held by the chunk filter.
	got := st.WriteFilteredChunkLocked(write, []byte("Retry(2)"))
	if len(got) != 0 {
		t.Fatalf("filtered = %q, want held (empty)", got)
	}
	if len(st.NoiseTrailing) == 0 {
		t.Fatal("NoiseTrailing empty — filter did not hold the prompt tail")
	}

	// Flush-boundary release puts it on screen verbatim.
	if n := st.FlushNoiseTrailingLocked(write); n != len("Retry(2)") {
		t.Fatalf("released %d bytes, want %d", n, len("Retry(2)"))
	}
	if string(written) != "Retry(2)" {
		t.Fatalf("written = %q, want %q", written, "Retry(2)")
	}
	if len(st.NoiseTrailing) != 0 {
		t.Fatalf("NoiseTrailing = %q after release, want empty", st.NoiseTrailing)
	}

	// Idempotent: a second boundary release emits nothing.
	if n := st.FlushNoiseTrailingLocked(write); n != 0 {
		t.Fatalf("second release returned %d, want 0", n)
	}
}

// TestFlushNoiseTrailingLockedSplitAcrossChunks covers a prompt split across
// two writes: the first chunk's fragment releases into the second chunk's
// filter input (normal carry), so only a true stream-tail hold reaches the
// boundary release.
func TestFlushNoiseTrailingLockedSplitAcrossChunks(t *testing.T) {
	st := &State{}
	var written []byte
	write := func(b []byte) { written = append(written, b...) }

	// "Retry(2" holds (pid-prefix tail); the second chunk completes the
	// shape so the carry re-holds the full fragment for the boundary.
	_ = st.WriteFilteredChunkLocked(write, []byte("Retry(2"))
	_ = st.WriteFilteredChunkLocked(write, []byte(")"))
	if string(written) != "" {
		t.Fatalf("written = %q before release, want held", written)
	}
	st.FlushNoiseTrailingLocked(write)
	if string(written) != "Retry(2)" {
		t.Fatalf("written = %q, want %q", written, "Retry(2)")
	}
}

// TestTakeFlushChunkLocked_PartialTakesAdvanceWithoutCorruption drives many
// sub-cap takes through the reslice-advance path: every take must return the
// exact next bytes in stream order, and bytes appended between takes must not
// disturb chunks already handed out.
func TestTakeFlushChunkLocked_PartialTakesAdvanceWithoutCorruption(t *testing.T) {
	st := &State{}
	var stream []byte
	for i := 0; i < 64; i++ {
		stream = append(stream, byte('a'+i%26), byte('0'+i%10), '\n')
	}
	st.PendingOutput = append(st.PendingOutput, stream...)
	// Interleaved appends land behind the remaining stream, so the expected
	// order is the original bytes followed by the appended markers.
	expected := append(append([]byte(nil), stream...), "!!!!!"...)

	var got []byte
	taken := [][]byte{}
	for iters := 0; len(st.PendingOutput) > 0; iters++ {
		chunk := st.TakeFlushChunkLocked(37)
		if len(chunk) == 0 {
			t.Fatal("partial take returned empty chunk")
		}
		taken = append(taken, chunk)
		got = append(got, chunk...)
		// Interleave appends on early takes so the resliced head coexists with
		// buffer growth; bounded so the loop still drains.
		if iters < 5 {
			st.PendingOutput = append(st.PendingOutput, '!')
		}
	}
	if !bytes.Equal(got, expected) {
		t.Fatalf("reassembled stream mismatch:\n got %q\nwant %q", got, expected)
	}
	// Chunks taken earlier are copies: later buffer growth must not have
	// written into them. FIFO order means each chunk covers the next
	// len(chunk) bytes of expected.
	off := 0
	for i, chunk := range taken {
		if off+len(chunk) > len(expected) || !bytes.Equal(chunk, expected[off:off+len(chunk)]) {
			t.Fatalf("chunk %d corrupted: got %q", i, chunk)
		}
		off += len(chunk)
	}
}

// TestTakeFlushChunkLocked_FullTakeAdoptsBuffer pins the zero-copy fast path:
// draining the entire buffer hands the backing array to the chunk (no
// allocation) and leaves the state empty.
func TestTakeFlushChunkLocked_FullTakeAdoptsBuffer(t *testing.T) {
	st := &State{}
	payload := []byte("full drain payload\n")
	st.PendingOutput = append(st.PendingOutput, payload...)

	chunk := st.TakeFlushChunkLocked(0)
	if !bytes.Equal(chunk, payload) {
		t.Fatalf("got %q, want %q", chunk, payload)
	}
	if len(st.PendingOutput) != 0 {
		t.Fatalf("expected drained buffer, got %d bytes", len(st.PendingOutput))
	}
	// The adopted chunk shares the buffer's old backing array — appending to
	// the state must allocate a fresh array rather than write into it.
	st.PendingOutput = append(st.PendingOutput, 'X')
	if string(chunk) != string(payload) {
		t.Fatalf("adopted chunk mutated by later append: %q", chunk)
	}
}
