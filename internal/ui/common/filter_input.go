package common

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// FilterInput is the shared `/` filter widget for list surfaces (Changes,
// dashboard, project tree). It owns the input-mode flag, the applied query,
// and the textinput; the owning model keeps the predicate and list rebuild —
// Update's `changed` result tells the model when to rebuild.
//
// Contract: `/` calls Start; while Active every KeyPressMsg routes to Update
// and is consumed (digits included — surfaces with digit bindings must gate
// them on Active, the tabs.go FilterActive pattern). Esc clears the query
// and exits; Enter exits keeping the query applied; any other key edits.
// Focus loss and workspace switches ExitKeep/Clear respectively.
type FilterInput struct {
	mode  bool
	query string
	input textinput.Model
}

// NewFilterInput creates a filter widget with the shared placeholder/limit.
func NewFilterInput() FilterInput {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100
	return FilterInput{input: ti}
}

// Active reports whether filter-input mode is on — keys route to Update.
func (f *FilterInput) Active() bool { return f.mode }

// Query is the applied substring. It survives Enter and blur: only Esc or
// Clear drops it.
func (f *FilterInput) Query() string { return f.query }

// Start enters input mode and returns the input's focus (blink) command.
func (f *FilterInput) Start() tea.Cmd {
	f.mode = true
	return f.input.Focus()
}

// ExitKeep leaves input mode keeping the query applied — Enter and blur.
func (f *FilterInput) ExitKeep() {
	f.mode = false
	f.input.Blur()
}

// Clear drops the query and exits input mode — Esc and surface resets
// (workspace switch) take this path.
func (f *FilterInput) Clear() {
	f.mode = false
	f.query = ""
	f.input.SetValue("")
	f.input.Blur()
}

// Update routes one message through the filter while Active. handled: the
// message was consumed (every KeyPressMsg is — that is what makes digit
// keys type into the filter). changed: the applied query changed and the
// list must rebuild — typed edits, paste, and the Esc clear; Enter exits
// without changing the query, so it reports no rebuild.
//
// Paste appends its first line (sanitized like every other single-line
// field) to the query — unlike the output dialog's search, a pasted
// newline does NOT accept: list filters apply live, so Enter owns accept.
func (f *FilterInput) Update(msg tea.Msg) (cmd tea.Cmd, handled, changed bool) {
	if !f.mode {
		return nil, false, false
	}
	if pm, ok := msg.(tea.PasteMsg); ok {
		return f.paste(pm.Content)
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, false, false
	}
	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		f.Clear()
		return nil, true, true
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		f.ExitKeep()
		return nil, true, false
	default:
		newInput, cmd := f.input.Update(msg)
		f.input = newInput
		f.query = f.input.Value()
		return cmd, true, true
	}
}

// paste appends the payload's first sanitized line to the query within the
// CharLimit — consumed but unchanged when the payload sanitizes to nothing
// or the input is already full. Caller guarantees mode is on.
func (f *FilterInput) paste(content string) (tea.Cmd, bool, bool) {
	content = PasteFirstLine(content)
	if content == "" {
		return nil, true, false
	}
	combined := f.input.Value() + content
	if limit := f.input.CharLimit; limit > 0 {
		if rs := []rune(combined); len(rs) > limit {
			combined = string(rs[:limit])
		}
	}
	if combined == f.input.Value() {
		return nil, true, false
	}
	f.input.SetValue(combined)
	f.query = combined
	return nil, true, true
}

// View renders the input line's text portion — callers render their own
// styled `/` or `filter:` prefix.
func (f *FilterInput) View() string { return f.input.View() }

// Focused reports whether the textinput holds focus — a test seam.
func (f *FilterInput) Focused() bool { return f.input.Focused() }

// Value is the textinput's current buffer. While Active it equals Query;
// after exit it can hold a stale value the next Start reuses or a reset
// clears — mirrors the pre-shared-widget surface behavior.
func (f *FilterInput) Value() string { return f.input.Value() }

// SetValue sets the input buffer without touching the applied query —
// a test seam and surface-reset hook; normal flow syncs query inside Update.
func (f *FilterInput) SetValue(s string) { f.input.SetValue(s) }

// SetQuery sets the applied query directly — a test seam; normal flow
// syncs query inside Update.
func (f *FilterInput) SetQuery(q string) { f.query = q }
