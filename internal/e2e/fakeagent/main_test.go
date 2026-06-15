package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// syncWriter is an in-memory syncer: it records every Write and counts Syncs so
// tests can assert recordStream flushes after each non-empty read.
type syncWriter struct {
	bytes.Buffer
	syncs int
}

func (w *syncWriter) Sync() error {
	w.syncs++
	return nil
}

func TestReadyBannerBytesEndsWithCRLF(t *testing.T) {
	got := readyBannerBytes()
	want := []byte("FAKEAGENT READY\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("readyBannerBytes() = %q, want %q", got, want)
	}
	// The trailing \r is the load-bearing part: a raw terminal performs no
	// NL->CRNL translation, so the banner must carry its own carriage return.
	if got[len(got)-2] != '\r' || got[len(got)-1] != '\n' {
		t.Fatalf("banner must end with CRLF, got %q", got)
	}
}

// TestRecordStreamPreservesEmbeddedCR is the bug-#2 regression sentinel: a
// payload carrying a literal carriage return (0x0D) must be recorded byte-for-
// byte. A regression to the named "Enter" key would never deliver 0x0D, so this
// guards the close-the-loop guarantee.
func TestRecordStreamPreservesEmbeddedCR(t *testing.T) {
	payload := []byte("hello\rworld\r")
	if !bytes.Contains(payload, []byte{0x0D}) {
		t.Fatal("test payload must contain an embedded CR")
	}

	var log syncWriter
	if err := recordStream(bytes.NewReader(payload), &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}

	if !bytes.Equal(log.Bytes(), payload) {
		t.Fatalf("recorded %q, want exact bytes %q", log.Bytes(), payload)
	}
	if bytes.Count(log.Bytes(), []byte{0x0D}) != 2 {
		t.Fatalf("expected 2 carriage returns preserved, got %q", log.Bytes())
	}
	if log.syncs == 0 {
		t.Fatal("recordStream never Synced; a polling test would race")
	}
}

// partialThenEOFReader yields a non-empty buffer and io.EOF in the SAME Read
// call, exercising the n>0-with-error path. recordStream must still record the
// partial buffer before returning.
type partialThenEOFReader struct {
	data []byte
	done bool
}

func (r *partialThenEOFReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, io.EOF
}

func TestRecordStreamRecordsPartialBufferOnEOF(t *testing.T) {
	payload := []byte("mid\rstream")
	var log syncWriter
	if err := recordStream(&partialThenEOFReader{data: payload}, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}
	if !bytes.Equal(log.Bytes(), payload) {
		t.Fatalf("partial buffer not recorded: got %q, want %q", log.Bytes(), payload)
	}
}

// errReader returns data then a non-EOF error; recordStream must surface that
// error (not swallow it like EOF) yet still record the bytes read with it.
type errReader struct {
	data []byte
	err  error
	done bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, r.err
}

func TestRecordStreamSurfacesNonEOFError(t *testing.T) {
	sentinel := errors.New("boom")
	payload := []byte("data\r")
	var log syncWriter
	err := recordStream(&errReader{data: payload, err: sentinel}, &log)
	if !errors.Is(err, sentinel) {
		t.Fatalf("recordStream err = %v, want %v", err, sentinel)
	}
	if !bytes.Equal(log.Bytes(), payload) {
		t.Fatalf("bytes read alongside error not recorded: got %q", log.Bytes())
	}
}

// chunkedReader yields each chunk in its own Read call, then io.EOF, so a test
// can exercise multiple non-empty reads in a single recordStream run.
type chunkedReader struct {
	chunks [][]byte
	i      int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.i])
	r.i++
	return n, nil
}

// TestRecordStreamSyncsPerNonEmptyRead pins the flush PLACEMENT: recordStream
// must Sync after every non-empty read, not once at end-of-stream. A regression
// that flushed a single time at EOF would still satisfy the weaker syncs>0 check
// in TestRecordStreamPreservesEmbeddedCR yet reintroduce the polling race this
// recorder exists to prevent, so assert one Sync per non-empty read.
func TestRecordStreamSyncsPerNonEmptyRead(t *testing.T) {
	chunks := [][]byte{[]byte("ab"), []byte("c\r")}
	var log syncWriter
	if err := recordStream(&chunkedReader{chunks: chunks}, &log); err != nil {
		t.Fatalf("recordStream returned error: %v", err)
	}
	if log.syncs != len(chunks) {
		t.Fatalf("expected one Sync per non-empty read (%d), got %d", len(chunks), log.syncs)
	}
	if got := log.String(); got != "abc\r" {
		t.Fatalf("recorded %q, want %q", got, "abc\r")
	}
}
