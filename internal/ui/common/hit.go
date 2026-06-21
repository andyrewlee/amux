package common

// TabBarHeight is the height, in rows, of the compact pane tab strip. Both the
// center and sidebar terminal panes render their tab strip as a single
// borderless line, and both use this when converting between screen and
// content coordinates.
const TabBarHeight = 1

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
