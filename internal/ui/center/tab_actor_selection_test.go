package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newSelectedTerminal returns a vterm that has `text` written into it and an
// active selection spanning the inclusive column range [startX, endX] on the
// first (absolute) line. The resulting terminal reports HasSelection()==true and
// SelectedText() returns the highlighted run, which is exactly what the copy /
// clear-and-notify handlers read.
func newSelectedTerminal(t *testing.T, text string, startX, endX int) *vterm.VTerm {
	t.Helper()
	term := vterm.New(40, 6)
	term.Write([]byte(text))
	term.SetSelection(startX, 0, endX, 0, true, false)
	if !term.HasSelection() {
		t.Fatalf("precondition: expected active selection after SetSelection")
	}
	return term
}

// ---------------------------------------------------------------------------
// handleSelectionClearAndNotify
//
// Behavior contract:
//   - Always clears the terminal selection, Tab.Selection, and the scroll FSM.
//   - Emits a tabSelectionResult only when notifyCopy is set, the terminal has a
//     live selection with non-empty text, AND a msgSink is wired.
// ---------------------------------------------------------------------------

func TestHandleSelectionClearAndNotify(t *testing.T) {
	type want struct {
		emitted   bool
		clipboard string
	}
	tests := []struct {
		name string
		// build the tab under test; helper-style so each case is self contained.
		build func(t *testing.T) *Tab
		// nilSink omits the msgSink entirely (exercises the nil-sink guard).
		nilSink    bool
		notifyCopy bool
		want       want
	}{
		{
			name: "notify with live selection emits selected text",
			build: func(t *testing.T) *Tab {
				return &Tab{Terminal: newSelectedTerminal(t, "hello world", 0, 4)}
			},
			notifyCopy: true,
			want:       want{emitted: true, clipboard: "hello"},
		},
		{
			name: "notifyCopy false suppresses emission but still clears",
			build: func(t *testing.T) *Tab {
				return &Tab{Terminal: newSelectedTerminal(t, "hello world", 0, 4)}
			},
			notifyCopy: false,
			want:       want{emitted: false},
		},
		{
			name: "no live terminal selection emits nothing",
			build: func(t *testing.T) *Tab {
				term := vterm.New(40, 6)
				term.Write([]byte("hello world"))
				// No SetSelection -> HasSelection() is false.
				return &Tab{
					Terminal: term,
					// A stale Tab.Selection must NOT cause a copy; only the live
					// terminal selection is the source of truth for the text.
					Selection: common.SelectionState{Active: true, StartX: 0, EndX: 4},
				}
			},
			notifyCopy: true,
			want:       want{emitted: false},
		},
		{
			name: "nil terminal is a safe no-op",
			build: func(t *testing.T) *Tab {
				return &Tab{Selection: common.SelectionState{Active: true, EndX: 3}}
			},
			notifyCopy: true,
			want:       want{emitted: false},
		},
		{
			name: "empty selected text emits nothing",
			build: func(t *testing.T) *Tab {
				// A zero-width selection over blank cells yields "" after the
				// trailing-space trim, so there is nothing to copy.
				term := vterm.New(40, 6)
				term.SetSelection(0, 0, 0, 0, true, false)
				return &Tab{Terminal: term}
			},
			notifyCopy: true,
			want:       want{emitted: false},
		},
		{
			name: "live selection but nil sink does not panic and clears",
			build: func(t *testing.T) *Tab {
				return &Tab{Terminal: newSelectedTerminal(t, "hello world", 0, 4)}
			},
			nilSink:    true,
			notifyCopy: true,
			want:       want{emitted: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := tt.build(t)
			// Seed mutable state so we can prove it is reset regardless of path.
			tab.Selection.Active = true
			tab.selectionScroll.SetDirection(-5, 10) // arm the scroll FSM

			var got []tea.Msg
			m := &Model{}
			if !tt.nilSink {
				m.msgSink = func(msg tea.Msg) { got = append(got, msg) }
			}

			m.handleSelectionClearAndNotify(tabEvent{
				tab:         tab,
				notifyCopy:  tt.notifyCopy,
				workspaceID: "ws-1",
				tabID:       TabID("tab-1"),
			})

			// Post-conditions that hold for every case: state is always reset.
			tab.mu.Lock()
			if tab.Selection != (common.SelectionState{}) {
				tab.mu.Unlock()
				t.Fatalf("expected Tab.Selection reset to zero, got %+v", tab.Selection)
			}
			if tab.Terminal != nil && tab.Terminal.HasSelection() {
				tab.mu.Unlock()
				t.Fatal("expected terminal selection cleared")
			}
			tab.mu.Unlock()
			if dir := tab.selectionScroll.ScrollDir; dir != 0 {
				t.Fatalf("expected selectionScroll reset (ScrollDir=0), got %d", dir)
			}

			if tt.want.emitted {
				if len(got) != 1 {
					t.Fatalf("expected exactly one tabSelectionResult, got %d: %#v", len(got), got)
				}
				res, ok := got[0].(tabSelectionResult)
				if !ok {
					t.Fatalf("expected tabSelectionResult, got %T", got[0])
				}
				if res.clipboard != tt.want.clipboard {
					t.Errorf("clipboard = %q, want %q", res.clipboard, tt.want.clipboard)
				}
				if res.workspaceID != "ws-1" {
					t.Errorf("workspaceID = %q, want %q", res.workspaceID, "ws-1")
				}
				if res.tabID != TabID("tab-1") {
					t.Errorf("tabID = %q, want %q", res.tabID, TabID("tab-1"))
				}
			} else if len(got) != 0 {
				t.Fatalf("expected no emission, got %d: %#v", len(got), got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleSelectionCopy
//
// Behavior contract:
//   - Reads the live terminal selection text and emits a tabSelectionResult when
//     notifyCopy is set, the terminal has a non-empty selection, and msgSink is
//     wired. Unlike clear-and-notify, it must NOT clear the selection — copy is a
//     non-destructive operation.
// ---------------------------------------------------------------------------

func TestHandleSelectionCopy(t *testing.T) {
	type want struct {
		emitted   bool
		clipboard string
	}
	tests := []struct {
		name       string
		build      func(t *testing.T) *Tab
		nilSink    bool
		notifyCopy bool
		want       want
	}{
		{
			name: "copy with live selection emits text and preserves selection",
			build: func(t *testing.T) *Tab {
				return &Tab{Terminal: newSelectedTerminal(t, "hello world", 6, 10)}
			},
			notifyCopy: true,
			want:       want{emitted: true, clipboard: "world"},
		},
		{
			name: "notifyCopy false emits nothing",
			build: func(t *testing.T) *Tab {
				return &Tab{Terminal: newSelectedTerminal(t, "hello world", 0, 4)}
			},
			notifyCopy: false,
			want:       want{emitted: false},
		},
		{
			name: "no live selection emits nothing",
			build: func(t *testing.T) *Tab {
				term := vterm.New(40, 6)
				term.Write([]byte("hello world"))
				return &Tab{Terminal: term}
			},
			notifyCopy: true,
			want:       want{emitted: false},
		},
		{
			name: "nil terminal is a safe no-op",
			build: func(t *testing.T) *Tab {
				return &Tab{}
			},
			notifyCopy: true,
			want:       want{emitted: false},
		},
		{
			name: "empty selected text emits nothing",
			build: func(t *testing.T) *Tab {
				term := vterm.New(40, 6)
				term.SetSelection(0, 0, 0, 0, true, false)
				return &Tab{Terminal: term}
			},
			notifyCopy: true,
			want:       want{emitted: false},
		},
		{
			name: "live selection but nil sink does not panic",
			build: func(t *testing.T) *Tab {
				return &Tab{Terminal: newSelectedTerminal(t, "hello world", 0, 4)}
			},
			nilSink:    true,
			notifyCopy: true,
			want:       want{emitted: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := tt.build(t)

			var got []tea.Msg
			m := &Model{}
			if !tt.nilSink {
				m.msgSink = func(msg tea.Msg) { got = append(got, msg) }
			}

			// Record whether the terminal carried a live selection before the copy
			// so we can assert copy is non-destructive.
			hadSelection := tab.Terminal != nil && tab.Terminal.HasSelection()

			m.handleSelectionCopy(tabEvent{
				tab:         tab,
				notifyCopy:  tt.notifyCopy,
				workspaceID: "ws-2",
				tabID:       TabID("tab-2"),
			})

			// Copy must never clear the live terminal selection.
			if hadSelection && (tab.Terminal == nil || !tab.Terminal.HasSelection()) {
				t.Fatal("expected copy to preserve the terminal selection")
			}

			if tt.want.emitted {
				if len(got) != 1 {
					t.Fatalf("expected exactly one tabSelectionResult, got %d: %#v", len(got), got)
				}
				res, ok := got[0].(tabSelectionResult)
				if !ok {
					t.Fatalf("expected tabSelectionResult, got %T", got[0])
				}
				if res.clipboard != tt.want.clipboard {
					t.Errorf("clipboard = %q, want %q", res.clipboard, tt.want.clipboard)
				}
				if res.workspaceID != "ws-2" {
					t.Errorf("workspaceID = %q, want %q", res.workspaceID, "ws-2")
				}
				if res.tabID != TabID("tab-2") {
					t.Errorf("tabID = %q, want %q", res.tabID, TabID("tab-2"))
				}
			} else if len(got) != 0 {
				t.Fatalf("expected no emission, got %d: %#v", len(got), got)
			}
		})
	}
}
