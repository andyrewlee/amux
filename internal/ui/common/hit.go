package common

// HitRegion represents a rectangular hit target in view-local coordinates.
type HitRegion struct {
	ID     string
	X      int
	Y      int
	Width  int
	Height int
}

// Contains reports whether the point is within the hit region bounds.
func (h HitRegion) Contains(x, y int) bool {
	return x >= h.X && x < h.X+h.Width && y >= h.Y && y < h.Y+h.Height
}
