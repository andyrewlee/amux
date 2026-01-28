package common

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (d *Dialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if !d.visible {
		return nil
	}

	lines := d.renderLines()
	if len(lines) == 0 {
		return nil
	}

	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range d.optionHits {
		if hit.region.Contains(localX, localY) {
			d.cursor = hit.cursorIndex
			d.visible = false

			switch d.dtype {
			case DialogInput:
				value := d.input.Value()
				return func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: hit.optionIndex == 0,
						Value:     value,
					}
				}
			case DialogConfirm:
				return func() tea.Msg {
					return DialogResult{ID: d.id, Confirmed: hit.optionIndex == 0}
				}
			case DialogSelect:
				value := d.options[hit.optionIndex]
				return func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: true,
						Index:     hit.optionIndex,
						Value:     value,
					}
				}
			case DialogMultiSelect:
				// For multi-select, toggle selection on click (don't close dialog)
				if hit.optionIndex == -1 {
					// "Done" button clicked
					indices, values := d.selectedValues()
					return func() tea.Msg {
						return DialogResult{
							ID:        d.id,
							Confirmed: true,
							Indices:   indices,
							Values:    values,
						}
					}
				}
				// Toggle selection for the clicked option
				if d.selected == nil {
					d.selected = make(map[int]bool)
				}
				if d.selected[hit.optionIndex] {
					delete(d.selected, hit.optionIndex)
				} else {
					d.selected[hit.optionIndex] = true
				}
				d.visible = true // Keep dialog open
				return nil
			}
		}
	}

	return nil
}
