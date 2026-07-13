package common

// settingsHeaderLines and settingsFooterLines encode a structural invariant
// of renderLines() in settings_render.go: it always begins with exactly two
// fixed lines ("Settings" + a blank) and always ends with exactly two fixed
// lines (a blank + "[Close]"), regardless of which optional sections
// (the update hint, the update affordance) are present. Everything between
// those two fixed slices is the scrollable body.
// TestSettingsRenderHeaderFooterInvariant guards this assumption.
const (
	settingsHeaderLines = 2
	settingsFooterLines = 2
)

// composeVisibleLines returns the lines SettingsDialog actually renders: the
// fixed header, a height-clamped and scroll-offset window of the body, and
// the fixed footer ("[Close]" and its border are always shown, never
// scrolled out of view). Modeled on the file picker's scrollOffset/
// maxVisible viewport (internal/ui/common/filepicker.go).
//
// It also rewrites s.hitRegions -- populated as a side effect of
// renderLines(), in full/unclamped coordinates -- into the coordinates of
// the composed/visible lines, dropping hit regions for rows currently
// scrolled out of view so clicks can't resolve against invisible rows.
func (s *SettingsDialog) composeVisibleLines() []string {
	full := s.renderLines()
	fullHits := s.hitRegions

	if len(full) < settingsHeaderLines+settingsFooterLines {
		// Not enough content to have a meaningful header/body/footer split;
		// this cannot happen with the current renderLines() implementation,
		// but render unclamped rather than risk a slice panic.
		return full
	}

	bodyLen := len(full) - settingsHeaderLines - settingsFooterLines
	visibleBody := s.bodyWindowHeight(bodyLen)
	offset := s.clampScrollOffset(fullHits, bodyLen, visibleBody)

	lines := make([]string, 0, settingsHeaderLines+visibleBody+settingsFooterLines)
	lines = append(lines, full[:settingsHeaderLines]...)
	bodyStart := settingsHeaderLines + offset
	lines = append(lines, full[bodyStart:bodyStart+visibleBody]...)
	lines = append(lines, full[len(full)-settingsFooterLines:]...)

	s.hitRegions = remapHitRegions(fullHits, len(full), offset, visibleBody)
	return lines
}

// bodyWindowHeight returns how many body rows fit given the dialog's
// assigned height. An unset height (0, as in tests that construct a dialog
// and call renderLines()/View() without ever calling SetSize) is treated as
// unbounded, so existing content-only assertions keep working without
// requiring every test to size the dialog first.
func (s *SettingsDialog) bodyWindowHeight(bodyLen int) int {
	if bodyLen <= 0 {
		return 0
	}
	if s.height <= 0 {
		return bodyLen
	}

	_, frameY, _, _ := s.dialogFrame()
	avail := s.height - frameY - settingsHeaderLines - settingsFooterLines
	if avail < 1 {
		avail = 1 // always show at least one body row alongside the footer
	}
	if avail > bodyLen {
		avail = bodyLen
	}
	return avail
}

// clampScrollOffset adjusts (and persists into s.scrollOffset) the first
// visible body row so the currently focused item's row stays inside the
// visible window -- the same ensure-visible shape as the file picker's
// ensureVisible, just evaluated fresh on every render instead of requiring
// each navigation handler (handleNext, handlePrev, handleClick, ...) to
// call it explicitly. If the focused item isn't part of the scrollable body
// (settingsItemClose lives in the fixed footer), the offset is left as-is:
// it keeps whatever position navigation last scrolled the body to.
func (s *SettingsDialog) clampScrollOffset(fullHits []settingsHitRegion, bodyLen, visibleBody int) int {
	if visibleBody >= bodyLen {
		s.scrollOffset = 0
		return 0
	}

	if idx := focusedBodyIndex(fullHits, s.focusedItem, s.themeCursor); idx >= 0 {
		switch {
		case idx < s.scrollOffset:
			s.scrollOffset = idx
		case idx >= s.scrollOffset+visibleBody:
			s.scrollOffset = idx - visibleBody + 1
		}
	}

	if maxOffset := bodyLen - visibleBody; s.scrollOffset > maxOffset {
		s.scrollOffset = maxOffset
	}
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
	return s.scrollOffset
}

// focusedBodyIndex finds the body-relative row (0-based, excluding the
// header) of the currently focused item using the hit regions renderLines
// already records for every focusable row. It returns -1 when the focused
// item has no body row (settingsItemClose, rendered in the fixed footer).
func focusedBodyIndex(hits []settingsHitRegion, focused settingsItem, themeCursor int) int {
	for _, h := range hits {
		if h.item != focused {
			continue
		}
		if focused == settingsItemTheme && h.index != themeCursor {
			continue
		}
		return h.region.Y - settingsHeaderLines
	}
	return -1
}

// remapHitRegions translates hit regions from renderLines' full, unclamped
// coordinates into the coordinates of the composed/visible lines, dropping
// any row currently scrolled out of the body window.
func remapHitRegions(hits []settingsHitRegion, fullLen, offset, visibleBody int) []settingsHitRegion {
	footerStart := fullLen - settingsFooterLines
	out := make([]settingsHitRegion, 0, len(hits))
	for _, h := range hits {
		y := h.region.Y
		switch {
		case y < settingsHeaderLines:
			// Header rows aren't focusable/clickable today, but keep the
			// mapping total (identity) in case that changes.
		case y >= footerStart:
			h.region.Y = settingsHeaderLines + visibleBody + (y - footerStart)
		default:
			bodyIdx := y - settingsHeaderLines
			if bodyIdx < offset || bodyIdx >= offset+visibleBody {
				continue // scrolled out of view: not clickable
			}
			h.region.Y = settingsHeaderLines + (bodyIdx - offset)
		}
		out = append(out, h)
	}
	return out
}
