package pty

const maxWinsizeDimension = 1<<16 - 1

// WinsizeFromInts converts UI dimensions into PTY winsize dimensions.
// Non-positive pairs mean "use the platform/default size" and return ok=false.
// Oversized dimensions are clamped to the largest value the PTY API accepts.
func WinsizeFromInts(rows, cols int) (uint16, uint16, bool) {
	if rows <= 0 || cols <= 0 {
		return 0, 0, false
	}
	return clampWinsizeDimension(rows), clampWinsizeDimension(cols), true
}

func clampWinsizeDimension(v int) uint16 {
	if v <= 0 {
		return 0
	}
	if v > maxWinsizeDimension {
		return maxWinsizeDimension
	}
	return uint16(v)
}
