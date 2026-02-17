package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

// Update handles messages
func (d *Dialog) Update(msg tea.Msg) (*Dialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if cmd := d.handleClick(msg); cmd != nil {
				return d, cmd
			}
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			d.visible = false
			return d, func() tea.Msg {
				return DialogResult{ID: d.id, Confirmed: false}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			logging.Info("Dialog Enter pressed: id=%s value=%s", d.id, d.input.Value())
			switch d.dtype {
			case DialogInput:
				// Block Enter if validation fails
				if d.validationErr != "" {
					return d, nil
				}
				d.visible = false
				value := d.input.Value()
				id := d.id
				logging.Info("Dialog returning InputResult: id=%s value=%s", id, value)
				return d, func() tea.Msg {
					return DialogResult{
						ID:        id,
						Confirmed: true,
						Value:     value,
					}
				}
			case DialogConfirm:
				d.visible = false
				return d, func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: d.cursor == 0,
					}
				}
			case DialogSelect:
				d.visible = false
				// For filtered dialogs, return the original index
				var originalIdx int
				var value string
				if d.filterEnabled && len(d.filteredIndices) > 0 {
					originalIdx = d.filteredIndices[d.cursor]
					value = d.options[originalIdx]
				} else if !d.filterEnabled && d.cursor < len(d.options) {
					originalIdx = d.cursor
					value = d.options[d.cursor]
				} else {
					// No valid selection
					d.visible = false
					return d, func() tea.Msg {
						return DialogResult{ID: d.id, Confirmed: false}
					}
				}
				return d, func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: true,
						Index:     originalIdx,
						Value:     value,
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down"))):
			if d.dtype != DialogInput {
				maxLen := len(d.options)
				if d.filterEnabled {
					maxLen = len(d.filteredIndices)
				}
				if maxLen > 0 {
					d.cursor = (d.cursor + 1) % maxLen
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up"))):
			if d.dtype != DialogInput {
				maxLen := len(d.options)
				if d.filterEnabled {
					maxLen = len(d.filteredIndices)
				}
				if maxLen > 0 {
					d.cursor--
					if d.cursor < 0 {
						d.cursor = maxLen - 1
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
			if d.dtype == DialogConfirm {
				d.cursor = 0
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right"))):
			if d.dtype == DialogConfirm {
				d.cursor = 1
			}
		}
	}

	// Update text input if applicable
	if d.dtype == DialogInput {
		// Transform incoming text if transform function is set
		if d.inputTransform != nil {
			msg = d.transformInputMsg(msg)
		}

		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)

		// Run validation if validator is set
		if d.inputValidate != nil {
			d.validationErr = d.inputValidate(d.input.Value())
		}

		return d, cmd
	}

	// Update filter input for filtered select dialogs
	if d.dtype == DialogSelect && d.filterEnabled {
		oldValue := d.filterInput.Value()
		var cmd tea.Cmd
		d.filterInput, cmd = d.filterInput.Update(msg)
		// Reapply filter if input changed
		if d.filterInput.Value() != oldValue {
			d.applyFilter()
		}
		return d, cmd
	}

	return d, nil
}

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

			switch d.dtype {
			case DialogInput:
				if hit.optionIndex == 0 && d.validationErr != "" {
					return nil
				}
				d.visible = false
				value := d.input.Value()
				return func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: hit.optionIndex == 0,
						Value:     value,
					}
				}
			case DialogConfirm:
				d.visible = false
				return func() tea.Msg {
					return DialogResult{ID: d.id, Confirmed: hit.optionIndex == 0}
				}
			case DialogSelect:
				d.visible = false
				value := d.options[hit.optionIndex]
				return func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: true,
						Index:     hit.optionIndex,
						Value:     value,
					}
				}
			}
		}
	}

	return nil
}
