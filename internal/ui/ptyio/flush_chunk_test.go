package ptyio

import "testing"

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
