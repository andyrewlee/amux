package common

import (
	"charm.land/lipgloss/v2"
)

// dialogBorderStyle returns the shared bordered dialog chrome style used by
// Dialog, SettingsDialog, and FilePicker: a rounded border in the primary
// color with (1,2) padding, sized to the given content width.
func dialogBorderStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary()).
		Padding(1, 2).
		Width(width)
}

// dialogFrameOffsets returns the frame size (border + padding) of the given
// dialog style and the corresponding half-offsets used to align content
// within that frame.
func dialogFrameOffsets(style lipgloss.Style) (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = style.GetFrameSize()
	offsetX = frameX / 2
	offsetY = frameY / 2
	return frameX, frameY, offsetX, offsetY
}

// centerDialogBounds centers a dialog of the given content width/height
// (plus frame) within a screen of screenW x screenH, clamping the resulting
// position to be non-negative.
func centerDialogBounds(screenW, screenH, contentW, frameX, frameY, contentHeight int) (x, y, w, h int) {
	w = contentW + frameX
	h = contentHeight + frameY
	x = (screenW - w) / 2
	y = (screenH - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}
