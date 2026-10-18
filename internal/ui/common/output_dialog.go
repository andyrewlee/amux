package common

import (
	"fmt"
	"strings"
	"unicode"

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
//
// Search is opt-in via SetSearchable: `/` opens a query field (literal
// case-insensitive substring over the retained snapshot only), `enter`
// accepts and jumps to the first match, `n`/`N` cycle matches. It is
// appropriate only for free-form transcript flavors (run output, script
// output) — fixed-field panels like workspace status leave it off.
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
	// Search state. While editing, the dialog consumes ALL input (keys and
	// paste) into the query field — nothing reaches the terminal or the
	// app-level `a` intercept. The app reads Editing() to gate those.
	searchable bool
	editing    bool
	query      string
	matches    []outputDialogMatch
	matchIdx   int  // selected match; -1 when query empty or no matches
	wrapped    bool // the most recent n/N jump wrapped (one-jump footer marker)
}

// outputDialogMatch and the search machinery (updateQueryEdit, jumpMatch,
// recomputeMatches, repairMatchSelection, …) live in output_dialog_search.go.

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
// preserved. An active query is re-run against the new snapshot; the
// selection repairs to the nearest surviving match.
func (d *OutputDialog) SetContent(content string) {
	d.lines = sanitizeOutputLines(content)
	if d.following {
		d.offset = len(d.lines)
	}
	d.clampOffset()
	if d.query != "" {
		d.recomputeMatches()
	}
}

// Following reports whether follow mode is on (SetContent pins to bottom).
func (d *OutputDialog) Following() bool { return d.following }

// SetAttachHint advertises the caller's `a` attach binding in the footer.
// The dialog does not handle `a` itself — the owning overlay slot does.
func (d *OutputDialog) SetAttachHint(on bool) { d.attachHint = on }

// SetSearchable enables the `/` query field and `n`/`N` match cycling.
// Enable only for free-form transcript flavors; fixed panels stay default.
func (d *OutputDialog) SetSearchable(on bool) { d.searchable = on }

// Editing reports whether the dialog is in query-edit mode — consuming all
// input including paste. The owning overlay uses it to gate app-level key
// intercepts (`a` attach) and paste fall-through.
func (d *OutputDialog) Editing() bool { return d.editing }

// Text returns the full retained snapshot — what `y` copy-out places on the
// clipboard. The dialog itself never copies; the owning overlay slot wires
// the key so it can toast the outcome.
func (d *OutputDialog) Text() string { return strings.Join(d.lines, "\n") }

func (d *OutputDialog) Update(msg tea.Msg) (*OutputDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	// Query-edit mode consumes all input and never emits a close result.
	if d.editing {
		return d.updateQueryEdit(msg)
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc", "enter"))):
		d.visible = false
		return d, func() tea.Msg { return OutputDialogResult{} }
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("/"))) && d.searchable:
		d.editing = true
		return d, nil
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("n"))):
		d.jumpMatch(1)
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("N"))):
		d.jumpMatch(-1)
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("f"))):
		d.following = !d.following
		d.wrapped = false
		if d.following {
			d.offset = len(d.lines)
		}
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("up", "k"))):
		d.following = false
		d.wrapped = false
		d.offset--
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("down", "j"))):
		d.following = false
		d.wrapped = false
		d.offset++
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("pgup"))):
		d.following = false
		d.wrapped = false
		d.offset -= d.viewCap
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("pgdown"))):
		d.following = false
		d.wrapped = false
		d.offset += d.viewCap
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("g"))):
		d.following = false
		d.wrapped = false
		d.offset = 0
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("G"))):
		// Jump-to-bottom doubles as follow-resume (tail -f convention).
		d.following = true
		d.wrapped = false
		d.offset = len(d.lines)
	}
	d.clampOffset()
	return d, nil
}

// TrimLastWord drops a trailing run of spaces then the preceding word —
// ctrl+w behavior in a query field.
func TrimLastWord(s string) string {
	rs := []rune(s)
	i := len(rs)
	for i > 0 && unicode.IsSpace(rs[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(rs[i-1]) {
		i--
	}
	return string(rs[:i])
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
	return dialogBorderStyle(w).Render(strings.Join(d.renderLines(w), "\n"))
}

func (d *OutputDialog) renderLines(w int) []string {
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
		matchLine, match := -1, outputDialogMatch{}
		if d.matchIdx >= 0 && d.matchIdx < len(d.matches) {
			matchLine = d.matches[d.matchIdx].line
			match = d.matches[d.matchIdx]
		}
		for i := d.offset; i < end; i++ {
			line := d.lines[i]
			if i == matchLine {
				line = highlightedLine(line, match)
			}
			lines = append(lines, line)
		}
	}
	return append(lines, "", d.renderFooter(w, capLines, muted))
}

// renderFooter builds the footer for the dialog's current mode. While
// editing it echoes the query (`/query█`) with the accept/done hints; in
// browse mode an accepted query shows `match k/N` (or `0 matches`) plus a
// `(wrapped)` marker on the jump that wrapped; otherwise the standard
// scroll/follow/close hints show.
func (d *OutputDialog) renderFooter(w, capLines int, muted lipgloss.Style) string {
	attach := ""
	if d.attachHint {
		attach = "a attach  "
	}
	accent := lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
	switch {
	case d.editing:
		hint := "  esc done · enter accept"
		echo := "/" + d.query + "█"
		// The echo is truncated by display CELLS (leading …, tail kept —
		// the newest chars matter): a rune-count slice against a cell
		// budget can start at a negative index on wide queries.
		if hintW := lipgloss.Width(hint); hintW > w {
			// The full hint cannot fit either: reserve one cell for the
			// cursor when there is any room, and shorten the hint into
			// what remains.
			switch {
			case w >= 2:
				echo = "█"
				hint = TruncateRightCells(hint, w-1, "…", 0)
			case w == 1:
				echo = "█"
				hint = ""
			default:
				echo, hint = "", ""
			}
		} else {
			switch budget := w - hintW - 2; {
			case budget >= 2:
				echo = TruncateLeftCells(echo, budget, "…", 0)
			case budget == 1:
				echo = "█"
			default:
				echo = ""
			}
		}
		return accent.Render(echo) + muted.Render(hint)
	case d.query != "":
		status := "0 matches"
		switch {
		case len(d.matches) > 0 && d.matchIdx < 0:
			// A refresh evicted the selected match and no survivor sat
			// at-or-after it — the selection is cleared but recoverable
			// (n re-selects the first match). Report the honest count;
			// "match 0/N" would claim a position that does not exist.
			status = fmt.Sprintf("%d matches", len(d.matches))
		case len(d.matches) > 0:
			status = fmt.Sprintf("match %d/%d", d.matchIdx+1, len(d.matches))
			if d.wrapped {
				status += " (wrapped)"
			}
		}
		prefix := ""
		if d.following {
			prefix = accent.Render("[following] ")
		}
		return prefix + accent.Render(status) + muted.Render("  n/N next/prev  "+attach+"y copy  f follow  esc close")
	}
	footer := "up/down scroll  " + attach + "y copy  f follow  esc close"
	if len(d.lines) > capLines {
		footer = "up/down scroll  pgup/pgdn page  G bottom  " + attach + "y copy  f follow  esc close"
	}
	if d.searchable {
		footer = "/ search  " + footer
	}
	if d.following {
		return accent.Render("[following] ") + muted.Render(footer)
	}
	return muted.Render(footer)
}
