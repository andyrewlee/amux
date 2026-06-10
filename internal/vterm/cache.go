package vterm

// Render-cache dirty tracking is epoch-based: every dirty mark advances a
// monotonic epoch counter and stamps the affected line; invalidation stamps a
// global epoch instead of touching every line. A line is dirty when its stamp
// (or the global stamp) is newer than the epoch recorded by the last clear.
// Compared to per-line bools plus an all-dirty flag, there is no state to
// forget to reset per line — clearing is a single counter assignment.

// bumpRenderEpoch advances the epoch counter and returns the new epoch.
func (v *VTerm) bumpRenderEpoch() uint64 {
	v.renderEpoch++
	return v.renderEpoch
}

func (v *VTerm) ensureRenderCache(height int) {
	if len(v.renderCache) != height {
		v.renderCache = make([]string, height)
		v.renderLineEpoch = make([]uint64, height)
		v.renderGlobalEpoch = v.bumpRenderEpoch()
	}
}

func (v *VTerm) markDirtyLine(y int) {
	if y < 0 || y >= v.Height {
		return
	}
	v.bumpVersion()
	if len(v.renderLineEpoch) == v.Height {
		v.renderLineEpoch[y] = v.bumpRenderEpoch()
	} else {
		v.renderGlobalEpoch = v.bumpRenderEpoch()
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
	if len(v.renderLineEpoch) == v.Height {
		epoch := v.bumpRenderEpoch()
		for y := start; y <= end; y++ {
			v.renderLineEpoch[y] = epoch
		}
		return
	}
	v.renderGlobalEpoch = v.bumpRenderEpoch()
}

func (v *VTerm) invalidateRenderCache() {
	v.renderCache = nil
	v.renderLineEpoch = nil
	v.renderGlobalEpoch = v.bumpRenderEpoch()
	v.bumpVersion()
}

// lineDirty reports whether line y is dirty relative to the last clear.
func (v *VTerm) lineDirty(y int) bool {
	if v.allDirty() {
		return true
	}
	if y < 0 || y >= len(v.renderLineEpoch) {
		return true
	}
	return v.renderLineEpoch[y] > v.renderCleanEpoch
}

// allDirty reports whether a global invalidation is newer than the last clear.
func (v *VTerm) allDirty() bool {
	return v.renderGlobalEpoch > v.renderCleanEpoch
}

// DirtyLines returns the dirty line flags and whether all lines are dirty.
// This is used by VTermLayer for optimized rendering. The returned slice is
// reused between calls; callers must not retain it across mutations.
func (v *VTerm) DirtyLines() ([]bool, bool) {
	// When scrolled, we can't use dirty tracking effectively
	if v.ViewOffset > 0 {
		return nil, true
	}
	// When sync is active, always redraw
	if v.syncActive {
		return nil, true
	}
	if v.allDirty() {
		return nil, true
	}
	if len(v.renderDirtyScratch) != len(v.renderLineEpoch) {
		v.renderDirtyScratch = make([]bool, len(v.renderLineEpoch))
	}
	for y := range v.renderLineEpoch {
		v.renderDirtyScratch[y] = v.renderLineEpoch[y] > v.renderCleanEpoch
	}
	return v.renderDirtyScratch, false
}

// ClearDirty resets dirty tracking state after a render.
func (v *VTerm) ClearDirty() {
	v.renderCleanEpoch = v.renderEpoch
}

// ClearDirtyWithCursor resets dirty tracking state and updates cursor tracking.
// This should be called after snapshotting to track cursor position changes.
func (v *VTerm) ClearDirtyWithCursor(showCursor bool) {
	v.ClearDirty()
	// Track cursor state for next frame's dirty detection
	v.lastShowCursor = showCursor
	v.lastCursorHidden = v.CursorHiddenForRender()
	v.lastCursorX = v.CursorX
	v.lastCursorY = v.CursorY
}

// RenderAndClear renders the terminal and marks the rendered state clean in
// one step, so callers cannot forget the clear that keeps the cache coherent.
func (v *VTerm) RenderAndClear() string {
	out := v.Render()
	v.ClearDirty()
	return out
}
