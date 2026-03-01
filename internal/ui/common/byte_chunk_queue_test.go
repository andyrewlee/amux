package common

import (
	"bytes"
	"testing"
)

func TestByteChunkQueuePopStopsAtChunkBoundary(t *testing.T) {
	var q ByteChunkQueue
	q.Append([]byte("abc"))
	q.Append([]byte("def"))

	got := q.Pop(5)
	if string(got) != "abc" {
		t.Fatalf("pop = %q, want %q", got, "abc")
	}
	if q.Len() != 3 {
		t.Fatalf("len = %d, want 3", q.Len())
	}

	rest := q.Pop(0)
	if string(rest) != "def" {
		t.Fatalf("rest = %q, want %q", rest, "def")
	}
	if q.Len() != 0 {
		t.Fatalf("len = %d, want 0", q.Len())
	}
}

func TestByteChunkQueueDropOldestPartial(t *testing.T) {
	var q ByteChunkQueue
	q.Append([]byte("abc"))
	q.Append([]byte("def"))

	dropped := q.DropOldest(4)
	if dropped != 4 {
		t.Fatalf("dropped = %d, want 4", dropped)
	}
	if q.Len() != 2 {
		t.Fatalf("len = %d, want 2", q.Len())
	}
	got := q.Pop(0)
	if string(got) != "ef" {
		t.Fatalf("remaining = %q, want %q", got, "ef")
	}
}

func TestByteChunkQueueDropOldestClearsWhenExhausted(t *testing.T) {
	var q ByteChunkQueue
	q.Append([]byte("hello"))

	if dropped := q.DropOldest(10); dropped != 5 {
		t.Fatalf("dropped = %d, want 5", dropped)
	}
	if q.Len() != 0 {
		t.Fatalf("len = %d, want 0", q.Len())
	}
	if out := q.Pop(1); len(out) != 0 {
		t.Fatalf("pop from empty queue = %v, want empty", out)
	}
}

func TestByteChunkQueueAppendClampsCapacity(t *testing.T) {
	var q ByteChunkQueue
	src := make([]byte, 4, 64)
	copy(src, []byte("data"))
	q.Append(src)

	out := q.Pop(4)
	if !bytes.Equal(out, []byte("data")) {
		t.Fatalf("pop = %q, want %q", out, "data")
	}
	if cap(out) != len(out) {
		t.Fatalf("cap(pop) = %d, want %d", cap(out), len(out))
	}
}
