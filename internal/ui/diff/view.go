package diff

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the diff viewer. The result is memoized on every render
// input — a static diff tab composing each frame reuses the last build.
func (m *Model) View() string {
	key := diffViewKey{
		diff:          m.diff,
		loading:       m.loading,
		scroll:        m.scroll,
		hunkIdx:       m.hunkIdx,
		wrap:          m.wrap,
		focused:       m.focused,
		width:         m.width,
		height:        m.height,
		stylesRev:     m.stylesRev,
		searching:     m.searching,
		query:         m.query,
		matchIdx:      m.matchIdx,
		searchWrapped: m.searchWrapped,
		matchCount:    len(m.matches),
	}
	if m.err != nil {
		key.errStr = m.err.Error()
	}
	if m.viewValid && m.viewKey == key {
		return m.viewCache
	}
	out := m.renderView()
	m.viewKey, m.viewCache, m.viewValid = key, out, true
	return out
}

// renderView performs the actual string build — the memoized half of View.
func (m *Model) renderView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.loading {
		return m.renderLoading()
	}

	if m.err != nil {
		return m.renderError()
	}

	if m.diff == nil {
		return m.renderEmpty()
	}

	if m.diff.Binary {
		return m.renderBinary()
	}

	if m.diff.Large {
		return m.renderLarge()
	}

	if m.diff.Empty || len(m.diff.Lines) == 0 {
		return m.renderNoChanges()
	}

	return m.renderDiff()
}

// renderLoading shows loading spinner
func (m *Model) renderLoading() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	loadingStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted()).
		Italic(true)
	b.WriteString(loadingStyle.Render("  Loading diff..."))

	return b.String()
}

// renderError shows error message
func (m *Model) renderError() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	errorStyle := lipgloss.NewStyle().
		Foreground(common.ColorError())
	b.WriteString(errorStyle.Render("  Error: " + common.SanitizeDisplayText(m.err.Error(), 512)))

	return b.String()
}

// renderEmpty shows empty state
func (m *Model) renderEmpty() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	emptyStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())
	b.WriteString(emptyStyle.Render("  No file selected"))

	return b.String()
}

// renderBinary shows binary file warning
func (m *Model) renderBinary() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	warningStyle := lipgloss.NewStyle().
		Foreground(common.ColorWarning()).
		Bold(true)
	b.WriteString(warningStyle.Render("  ⚠ Binary file - cannot display diff"))

	return b.String()
}

// renderLarge shows large file warning
func (m *Model) renderLarge() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	warningStyle := lipgloss.NewStyle().
		Foreground(common.ColorWarning()).
		Bold(true)
	b.WriteString(warningStyle.Render("  ⚠ File too large to display (> 2MB)"))

	return b.String()
}

// renderNoChanges shows no changes message
func (m *Model) renderNoChanges() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	emptyStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())
	b.WriteString(emptyStyle.Render("  No changes to display"))

	return b.String()
}

// renderHeader renders the file header
func (m *Model) renderHeader() string {
	path := ""
	if m.change != nil {
		path = common.SanitizeDisplayText(m.change.Path, 256)
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorPrimary())

	modeStr := ""
	switch m.mode {
	case git.DiffModeStaged:
		modeStr = " (staged)"
	case git.DiffModeUnstaged:
		modeStr = " (unstaged)"
	case git.DiffModeBranch:
		modeStr = " (branch)"
	}

	return headerStyle.Render(path + modeStr)
}

// clipChrome bounds one rendered chrome row (header/stats/footer) to the
// viewport width with ANSI-aware truncation.
func (m *Model) clipChrome(s string) string {
	return ansi.Truncate(s, m.width, "")
}

// renderDiff renders the diff content as a slice of prebuilt visual rows.
// Chrome tiers: height ≥3 shows header + stats + footer; height 2 drops the
// stats row; height 1 keeps only the header — no content rows exist below
// height 3. Nonpositive dimensions are already handled by renderView.
func (m *Model) renderDiff() string {
	var b strings.Builder

	b.WriteString(m.clipChrome(m.renderHeader()))

	// Stats line
	if m.height >= 3 && m.diff != nil {
		added := m.diff.AddedLines()
		deleted := m.diff.DeletedLines()
		statsStyle := lipgloss.NewStyle().Foreground(common.ColorMuted())

		addStyle := lipgloss.NewStyle().Foreground(common.ColorSuccess())
		delStyle := lipgloss.NewStyle().Foreground(common.ColorError())

		stats := addStyle.Render("+"+strconv.Itoa(added)) + " " +
			delStyle.Render("-"+strconv.Itoa(deleted))

		if len(m.diff.Hunks) > 0 {
			stats += statsStyle.Render(fmt.Sprintf("  (%d hunks)", len(m.diff.Hunks)))
		}

		b.WriteString("\n")
		b.WriteString(m.clipChrome(stats))
	}

	// Content: exactly the selected visual-row slice, padded to capacity.
	rows := m.rows()
	capacity := m.contentHeight()
	emitted := 0
	for i := m.scroll; i < len(rows.rows) && emitted < capacity; i++ {
		row := rows.rows[i]
		b.WriteString("\n")
		if m.matchIdx >= 0 && m.matchIdx < len(m.matches) {
			b.WriteString(m.renderRowMatch(row, m.matches[m.matchIdx]))
		} else {
			b.WriteString(row.text)
		}
		emitted++
	}
	for i := emitted; i < capacity; i++ {
		b.WriteString("\n")
	}

	if m.height >= 2 {
		b.WriteString("\n")
		b.WriteString(m.clipChrome(m.renderFooter()))
	}

	return b.String()
}

// renderFooter renders the footer with keybindings and scroll info. While
// the query field is editing it echoes `/query█`; with an accepted query it
// shows `match k/N` (or `0 matches`) plus a `(wrapped)` marker on the jump
// that wrapped — the output viewers' footer contract.
func (m *Model) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())

	if m.searching {
		return footerStyle.Render("/"+m.query+"█") + footerStyle.Render("  esc done · enter accept")
	}

	var parts []string

	// Scroll position in visual rows — the same space scrolling moves in,
	// so the count stays truthful when wrapping expands a line.
	if m.diff != nil && len(m.diff.Lines) > 0 {
		total := len(m.rows().rows)
		pos := m.scroll + 1
		if pos > total {
			pos = total
		}
		parts = append(parts, fmt.Sprintf("%d/%d", pos, total))
	}

	// Hunk info
	if m.diff != nil && len(m.diff.Hunks) > 0 {
		parts = append(parts, fmt.Sprintf("hunk %d/%d", m.hunkIdx+1, len(m.diff.Hunks)))
	}

	// Match position — `match k/N`, `0 matches`, `(wrapped)` on the wrap jump.
	if m.query != "" {
		if len(m.matches) > 0 && m.matchIdx >= 0 {
			hit := fmt.Sprintf("match %d/%d", m.matchIdx+1, len(m.matches))
			if m.searchWrapped {
				hit += " (wrapped)"
			}
			parts = append(parts, hit)
		} else {
			parts = append(parts, "0 matches")
		}
	}

	// Wrap indicator
	if m.wrap {
		parts = append(parts, "[wrap]")
	}

	// Keybindings — n/N's meaning flips while a query has matches.
	keyStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary())
	navHint := keyStyle.Render("n/p") + ":hunk"
	if m.matchIdx >= 0 {
		navHint = keyStyle.Render("n/N") + ":match"
	}
	helpItems := []string{
		keyStyle.Render("j/k") + ":scroll",
		keyStyle.Render("/") + ":search",
		navHint,
		keyStyle.Render("w") + ":wrap",
		keyStyle.Render("q") + ":close",
	}

	return footerStyle.Render(strings.Join(parts, " | ")) + "  " + footerStyle.Render(strings.Join(helpItems, " "))
}
