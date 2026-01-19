package vterm

func (v *VTerm) ensureRenderCache(height int) {
	if len(v.renderCache) != height {
		v.renderCache = make([]string, height)
		v.renderDirty = make([]bool, height)
		v.renderDirtyAll = true
	}
}

func (v *VTerm) markDirtyLine(y int) {
	if y < 0 || y >= v.Height {
		return
	}
	v.bumpVersion()
	if len(v.renderDirty) == v.Height {
		v.renderDirty[y] = true
	} else {
		v.renderDirtyAll = true
	}
}

func (v *VTerm) markDirtyRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end >= v.Height {
		end = v.Height - 1
	}
	if start > end {
		return
	}
	v.bumpVersion()
	if len(v.renderDirty) == v.Height {
		for y := start; y <= end; y++ {
			v.renderDirty[y] = true
		}
		return
	}
	v.renderDirtyAll = true
}

func (v *VTerm) invalidateRenderCache() {
	v.renderCache = nil
	v.renderDirty = nil
	v.renderDirtyAll = true
	v.bumpVersion()
}

// DirtyLines returns the dirty line flags and whether all lines are dirty.
// This is used by VTermLayer for optimized rendering.
func (v *VTerm) DirtyLines() ([]bool, bool) {
	// When scrolled, we can't use dirty tracking effectively
	if v.ViewOffset > 0 {
		return nil, true
	}
	// When sync is active, always redraw
	if v.syncActive {
		return nil, true
	}
	return v.renderDirty, v.renderDirtyAll
}

// ClearDirty resets dirty tracking state after a render.
func (v *VTerm) ClearDirty() {
	v.renderDirtyAll = false
	for i := range v.renderDirty {
		v.renderDirty[i] = false
	}
}

// ClearDirtyWithCursor resets dirty tracking state and updates cursor tracking.
// This should be called after snapshotting to track cursor position changes.
func (v *VTerm) ClearDirtyWithCursor(showCursor bool) {
	v.renderDirtyAll = false
	for i := range v.renderDirty {
		v.renderDirty[i] = false
	}
	// Track cursor state for next frame's dirty detection
	v.lastShowCursor = showCursor
	v.lastCursorHidden = v.CursorHidden
	v.lastCursorX = v.CursorX
	v.lastCursorY = v.CursorY
}
