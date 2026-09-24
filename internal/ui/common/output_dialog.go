package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// OutputDialogResult is sent when a read-only output dialog closes. Any
// dismissal (Esc or Enter) closes it; there is nothing to confirm.
type OutputDialogResult struct{}

// OutputDialog is a modal, read-only viewer for captured command output —
// the workspace run script's pane tail. Up/Down/PgUp/PgDn scroll; Esc or
// Enter closes. Callers refresh the snapshot via SetContent; `f` toggles
// follow mode, which pins the view to the bottom on every refresh (standard
// tail -f UX: any manual scroll disengages, G re-engages).
type OutputDialog struct {
	visible bool
	width   int
	height  int

	title     string
	lines     []string
	offset    int  // first visible content line
	viewCap   int  // content lines per page, derived from height at SetSize
	following bool // pin offset to bottom on SetContent until a manual scroll
	// attachHint advertises the app-level `a` (attach live session) binding;
	// only the live-run flavor sets it — the dialog itself never handles `a`.
	attachHint bool
}

// outputDialogMaxLineRunes bounds a single content line — generous; the
// dialog's ~90-column width clips visually anyway.
const outputDialogMaxLineRunes = 512

// NewOutputDialog builds a viewer for content titled title. A nil/empty
// content renders a muted placeholder rather than a blank box. Content is
// untrusted (script output, repo-derived fields): lines are sanitized at
// ingestion so a stored line is always a rendered-safe line.
func NewOutputDialog(title, content string) *OutputDialog {
	return &OutputDialog{title: title, lines: sanitizeOutputLines(content)}
}

// sanitizeOutputLines splits content into lines and sanitizes each — the
// '\n' separators are the caller's own, but escapes/control bytes inside a
// line are stripped so subprocess output can't inject sequences or fabricate
// rows into the dialog.
func sanitizeOutputLines(content string) []string {
	if content == "" {
		return nil
	}
	raw := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for i, line := range raw {
		raw[i] = SanitizeDisplayText(line, outputDialogMaxLineRunes)
	}
	return raw
}

func (d *OutputDialog) Show()         { d.visible = true }
func (d *OutputDialog) Hide()         { d.visible = false }
func (d *OutputDialog) Visible() bool { return d.visible }
func (d *OutputDialog) Cursor() *tea.Cursor {
	return nil
}

// SetSize records the screen size so View can page the content; height is
// the only dimension the scroller needs.
func (d *OutputDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
	// Title + blank + footer ≈ 4 non-content lines inside the border.
	d.viewCap = max(4, min(30, h-8))
	d.clampOffset()
}

// SetContent replaces the viewed text (e.g. a refreshed capture) and clamps
// the scroll offset to the new length — pinned to the bottom while
// following, clamped in place otherwise so a scrolled reader's position is
// preserved.
func (d *OutputDialog) SetContent(content string) {
	d.lines = sanitizeOutputLines(content)
	if d.following {
		d.offset = len(d.lines)
	}
	d.clampOffset()
}

// Following reports whether follow mode is on (SetContent pins to bottom).
func (d *OutputDialog) Following() bool { return d.following }

// SetAttachHint advertises the caller's `a` attach binding in the footer.
// The dialog does not handle `a` itself — the owning overlay slot does.
func (d *OutputDialog) SetAttachHint(on bool) { d.attachHint = on }

func (d *OutputDialog) Update(msg tea.Msg) (*OutputDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc", "enter"))):
		d.visible = false
		return d, func() tea.Msg { return OutputDialogResult{} }
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("f"))):
		d.following = !d.following
		if d.following {
			d.offset = len(d.lines)
		}
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("up", "k"))):
		d.following = false
		d.offset--
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("down", "j"))):
		d.following = false
		d.offset++
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("pgup"))):
		d.following = false
		d.offset -= d.viewCap
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("pgdown"))):
		d.following = false
		d.offset += d.viewCap
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("g"))):
		d.following = false
		d.offset = 0
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("G"))):
		// Jump-to-bottom doubles as follow-resume (tail -f convention).
		d.following = true
		d.offset = len(d.lines)
	}
	d.clampOffset()
	return d, nil
}

func (d *OutputDialog) clampOffset() {
	maxOff := len(d.lines) - d.viewCap
	if maxOff < 0 {
		maxOff = 0
	}
	if d.offset > maxOff {
		d.offset = maxOff
	}
	if d.offset < 0 {
		d.offset = 0
	}
}

func (d *OutputDialog) View() string {
	if !d.visible {
		return ""
	}
	w := 60
	if d.width > 0 {
		w = min(90, max(50, d.width-16))
	}
	return dialogBorderStyle(w).Render(strings.Join(d.renderLines(), "\n"))
}

func (d *OutputDialog) renderLines() []string {
	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary())
	muted := lipgloss.NewStyle().Foreground(ColorMuted())

	lines := []string{title.Render(SanitizeDisplayText(d.title, dialogMaxTitleRunes)), ""}
	capLines := d.viewCap
	if capLines < 1 {
		capLines = 10
	}
	if len(d.lines) == 0 {
		lines = append(lines, muted.Render("(no output)"))
	} else {
		end := d.offset + capLines
		if end > len(d.lines) {
			end = len(d.lines)
		}
		lines = append(lines, d.lines[d.offset:end]...)
	}
	attach := ""
	if d.attachHint {
		attach = "a attach  "
	}
	footer := "up/down scroll  " + attach + "f follow  esc close"
	if len(d.lines) > capLines {
		footer = "up/down scroll  pgup/pgdn page  G bottom  " + attach + "f follow  esc close"
	}
	if d.following {
		footer = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true).Render("[following] ") + muted.Render(footer)
	} else {
		footer = muted.Render(footer)
	}
	return append(lines, "", footer)
}
