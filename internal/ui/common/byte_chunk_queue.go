package common

// ByteChunkQueue is a FIFO queue for immutable byte chunks.
// It avoids O(n) front-shifts and can pop fixed-size windows.
type ByteChunkQueue struct {
	chunks [][]byte
	bytes  int
}

// Append enqueues data without copying.
func (q *ByteChunkQueue) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	// Clamp capacity so queue ownership doesn't retain oversized backing arrays.
	data = data[:len(data):len(data)]
	q.chunks = append(q.chunks, data)
	q.bytes += len(data)
}

// Len returns the total queued byte count.
func (q *ByteChunkQueue) Len() int {
	return q.bytes
}

// Clear drops all queued data.
func (q *ByteChunkQueue) Clear() {
	q.chunks = nil
	q.bytes = 0
}

// DropOldest removes up to n bytes from the queue head and returns bytes dropped.
func (q *ByteChunkQueue) DropOldest(n int) int {
	if n <= 0 || q.bytes == 0 {
		return 0
	}
	dropped := 0
	for n > 0 && len(q.chunks) > 0 {
		head := q.chunks[0]
		if len(head) <= n {
			dropped += len(head)
			n -= len(head)
			q.chunks = q.chunks[1:]
			continue
		}
		dropped += n
		q.chunks[0] = head[n:]
		n = 0
	}
	q.bytes -= dropped
	if q.bytes <= 0 {
		q.Clear()
	}
	return dropped
}

// Pop dequeues up to limit bytes from the queue head chunk.
// It never coalesces across multiple chunks.
func (q *ByteChunkQueue) Pop(limit int) []byte {
	if q.bytes == 0 || len(q.chunks) == 0 {
		return nil
	}
	head := q.chunks[0]
	if limit <= 0 || limit > len(head) {
		limit = len(head)
	}
	out := head[:limit]
	if len(head) == limit {
		q.chunks = q.chunks[1:]
	} else {
		q.chunks[0] = head[limit:]
	}
	q.bytes -= limit
	if q.bytes == 0 {
		q.chunks = nil
	}
	return out
}
