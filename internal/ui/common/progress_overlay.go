package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ProgressOverlay renders a centered modal showing step-based progress with a spinner.
type ProgressOverlay struct {
	title        string
	steps        []string
	currentStep  int
	detail       string // shown in brackets after the active step
	spinnerFrame int
	visible      bool
	width        int
	height       int
}

// NewProgressOverlay creates a new progress overlay with the given title and step labels.
func NewProgressOverlay(title string, steps []string) *ProgressOverlay {
	return &ProgressOverlay{
		title:   title,
		steps:   steps,
		visible: true,
	}
}

// Show makes the overlay visible.
func (p *ProgressOverlay) Show() { p.visible = true }

// Hide hides the overlay.
func (p *ProgressOverlay) Hide() { p.visible = false }

// Visible returns whether the overlay is visible.
func (p *ProgressOverlay) Visible() bool { return p.visible }

// AdvanceStep moves to the next step, clearing the detail.
func (p *ProgressOverlay) AdvanceStep() {
	p.detail = ""
	if p.currentStep < len(p.steps)-1 {
		p.currentStep++
	}
}

// SetStepDetail sets the bracketed detail text shown after the active step.
func (p *ProgressOverlay) SetStepDetail(detail string) { p.detail = detail }

// TickSpinner advances the spinner frame.
func (p *ProgressOverlay) TickSpinner() { p.spinnerFrame++ }

// SetSize stores the terminal dimensions for centering.
func (p *ProgressOverlay) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// View renders the overlay box.
func (p *ProgressOverlay) View() string {
	if !p.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	checkStyle := lipgloss.NewStyle().
		Foreground(ColorSuccess)

	spinnerStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary)

	detailStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	futureStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	var lines []string
	lines = append(lines, titleStyle.Render(p.title))

	for i, step := range p.steps {
		var line string
		if i < p.currentStep {
			// Completed
			line = checkStyle.Render(Icons.Clean) + "  " + step
		} else if i == p.currentStep {
			// Active — append detail in brackets if set
			stepText := step
			if p.detail != "" {
				stepText += " " + detailStyle.Render("("+p.detail+")")
			}
			line = spinnerStyle.Render(SpinnerFrame(p.spinnerFrame)) + "  " + stepText
		} else {
			// Future
			line = futureStyle.Render("   " + step)
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Width(42)

	return boxStyle.Render(content)
}
