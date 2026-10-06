package common

import "charm.land/lipgloss/v2"

// envDialogHeaderLines and envDialogFooterLines encode a structural
// invariant of renderLines() in env_dialog.go: it always begins with exactly
// two fixed lines (the title + a blank) and always ends with exactly two
// fixed lines (a blank + the mode's key hints), in both list and add mode.
// Everything between those two fixed slices is the scrollable body — the
// env rows plus, while adding, the new-entry block (blank, "New entry",
// name row, value row, optional error row).
const (
	envDialogHeaderLines = 2
	envDialogFooterLines = 2
)

// composeVisibleLines returns the lines EnvDialog actually renders: the
// fixed header, a height-clamped and scroll-offset window of the body, and
// the fixed footer (the key hints are always shown, never scrolled out of
// view). Modeled on SettingsDialog.composeVisibleLines
// (settings_scroll.go) — env rows have no hit regions, so there is nothing
// to remap.
func (d *EnvDialog) composeVisibleLines() []string {
	full := d.renderLines()

	if len(full) < envDialogHeaderLines+envDialogFooterLines {
		// Not enough content for a header/body/footer split; cannot happen
		// with the current renderLines(), but render unclamped rather than
		// risk a slice panic.
		return full
	}

	bodyLen := len(full) - envDialogHeaderLines - envDialogFooterLines
	visibleBody := d.bodyWindowHeight(bodyLen, full)
	offset := d.clampScrollOffset(bodyLen, visibleBody)

	lines := make([]string, 0, envDialogHeaderLines+visibleBody+envDialogFooterLines)
	lines = append(lines, full[:envDialogHeaderLines]...)
	bodyStart := envDialogHeaderLines + offset
	lines = append(lines, full[bodyStart:bodyStart+visibleBody]...)
	lines = append(lines, full[len(full)-envDialogFooterLines:]...)
	return lines
}

// bodyWindowHeight returns how many body rows fit given the dialog's
// assigned height. An unset height (0, as in tests that construct a dialog
// and call View() without SetSize) is treated as unbounded so content-only
// assertions keep working.
//
// The fixed header/footer are measured with their real wrapped height, not
// their line count: the list-mode hint is longer than the dialog's max
// content width and always wraps to two rows, so counting logical lines
// would overrun the frame by one row on every render. Body rows are bounded
// by line count, matching the settings exemplar — a very long env value can
// still wrap inside its row, the same residual settings accepts.
func (d *EnvDialog) bodyWindowHeight(bodyLen int, full []string) int {
	if bodyLen <= 0 {
		return 0
	}
	if d.height <= 0 {
		return bodyLen
	}

	_, frameY, _, _ := dialogFrameOffsets(d.dialogStyle())
	chrome := wrappedLineHeight(full[:envDialogHeaderLines], d.dialogContentWidth()) +
		wrappedLineHeight(full[len(full)-envDialogFooterLines:], d.dialogContentWidth())
	avail := d.height - frameY - chrome
	if avail < 1 {
		avail = 1 // always show at least one body row alongside the footer
	}
	if avail > bodyLen {
		avail = bodyLen
	}
	return avail
}

// wrappedLineHeight returns the number of visual rows lines occupy when
// rendered at the dialog's content width — i.e. how many times each line
// wraps under the dialogStyle's Width constraint.
func wrappedLineHeight(lines []string, contentWidth int) int {
	style := lipgloss.NewStyle().Width(contentWidth)
	h := 0
	for _, l := range lines {
		h += lipgloss.Height(style.Render(l))
	}
	return h
}

// clampScrollOffset adjusts (and persists into d.scrollOffset) the first
// visible body row so the focused row stays inside the visible window —
// the same ensure-visible shape as SettingsDialog.clampScrollOffset and the
// file picker's ensureVisible, evaluated fresh on every render so callers
// don't have to call it from each navigation path.
func (d *EnvDialog) clampScrollOffset(bodyLen, visibleBody int) int {
	if visibleBody >= bodyLen {
		d.scrollOffset = 0
		return 0
	}

	if idx := d.focusedBodyIndex(); idx >= 0 {
		switch {
		case idx < d.scrollOffset:
			d.scrollOffset = idx
		case idx >= d.scrollOffset+visibleBody:
			d.scrollOffset = idx - visibleBody + 1
		}
	}

	if maxOffset := bodyLen - visibleBody; d.scrollOffset > maxOffset {
		d.scrollOffset = maxOffset
	}
	if d.scrollOffset < 0 {
		d.scrollOffset = 0
	}
	return d.scrollOffset
}

// focusedBodyIndex returns the body-relative row (0-based, excluding the
// header) that must stay visible: the env-row cursor in list mode, or the
// active add field's row while adding — the name row sits after the env
// rows plus the add block's blank/"New entry" lead-in, and the value row is
// one below it. Anchoring on the focused field keeps the rest of the add
// block (including the error line under the value row) inside the window
// whenever the window is at least a few rows tall. Returns -1 when there is
// nothing focusable in the body (empty list, not adding).
func (d *EnvDialog) focusedBodyIndex() int {
	if d.adding {
		return len(d.keys) + 2 + d.addField
	}
	if len(d.keys) == 0 {
		return -1
	}
	return d.cursor
}
