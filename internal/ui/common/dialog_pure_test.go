package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDialogSetInputTransform(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")

	if d.inputTransform != nil {
		t.Fatalf("expected nil transform on fresh input dialog")
	}

	upper := func(s string) string { return strings.ToUpper(s) }
	got := d.SetInputTransform(upper)

	if got != d {
		t.Fatalf("SetInputTransform should return the same dialog for chaining, got %p want %p", got, d)
	}
	if d.inputTransform == nil {
		t.Fatalf("expected transform to be stored")
	}
	if out := d.inputTransform("abc"); out != "ABC" {
		t.Fatalf("stored transform produced %q, want %q", out, "ABC")
	}

	// Setting nil clears the transform.
	d.SetInputTransform(nil)
	if d.inputTransform != nil {
		t.Fatalf("expected transform to be cleared when set to nil")
	}
}

func TestDialogSetInputValidate(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")

	if d.inputValidate != nil {
		t.Fatalf("expected nil validate on fresh input dialog")
	}

	nonEmpty := func(s string) string {
		if s == "" {
			return "required"
		}
		return ""
	}
	got := d.SetInputValidate(nonEmpty)

	if got != d {
		t.Fatalf("SetInputValidate should return the same dialog for chaining, got %p want %p", got, d)
	}
	if d.inputValidate == nil {
		t.Fatalf("expected validate to be stored")
	}
	if msg := d.inputValidate(""); msg != "required" {
		t.Fatalf("stored validate produced %q, want %q", msg, "required")
	}
	if msg := d.inputValidate("ok"); msg != "" {
		t.Fatalf("stored validate produced %q for valid input, want empty", msg)
	}

	// Chaining both setters returns the same dialog.
	if d.SetInputTransform(nil).SetInputValidate(nil) != d {
		t.Fatalf("chained setters should return the same dialog")
	}
	if d.inputValidate != nil {
		t.Fatalf("expected validate to be cleared when set to nil")
	}
}

func TestDialogTransformInputMsgKeyPress(t *testing.T) {
	tests := []struct {
		name      string
		transform InputTransformFunc
		inText    string
		wantText  string
		// wantSame asserts the returned msg is the original (unchanged) value.
		wantSame bool
	}{
		{
			name:      "transform uppercases text",
			transform: strings.ToUpper,
			inText:    "abc",
			wantText:  "ABC",
			wantSame:  false,
		},
		{
			name:      "transform that is a no-op returns original msg",
			transform: func(s string) string { return s },
			inText:    "abc",
			wantText:  "abc",
			wantSame:  true,
		},
		{
			name:      "empty key text is skipped",
			transform: func(string) string { return "X" },
			inText:    "",
			wantText:  "",
			wantSame:  true,
		},
		{
			name:      "transform strips spaces",
			transform: func(s string) string { return strings.ReplaceAll(s, " ", "") },
			inText:    "a b",
			wantText:  "ab",
			wantSame:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewInputDialog("id", "Title", "Placeholder")
			d.SetInputTransform(tt.transform)

			in := tea.KeyPressMsg{Code: 'a', Text: tt.inText}
			out := d.transformInputMsg(in)

			key, ok := out.(tea.KeyPressMsg)
			if !ok {
				t.Fatalf("expected KeyPressMsg back, got %T", out)
			}
			if key.Text != tt.wantText {
				t.Fatalf("transformed text = %q, want %q", key.Text, tt.wantText)
			}
			if tt.wantSame && out != tea.Msg(in) {
				t.Fatalf("expected original msg to pass through unchanged")
			}
			// Fields other than Text must be preserved.
			if key.Code != in.Code {
				t.Fatalf("Code changed: got %v want %v", key.Code, in.Code)
			}
		})
	}
}

func TestDialogTransformInputMsgPaste(t *testing.T) {
	tests := []struct {
		name        string
		transform   InputTransformFunc
		inContent   string
		wantContent string
		wantSame    bool
	}{
		{
			name:        "transform uppercases paste content",
			transform:   strings.ToUpper,
			inContent:   "hello",
			wantContent: "HELLO",
			wantSame:    false,
		},
		{
			name:        "no-op transform returns original paste msg",
			transform:   func(s string) string { return s },
			inContent:   "hello",
			wantContent: "hello",
			wantSame:    true,
		},
		{
			name:        "empty paste content with mutating transform",
			transform:   func(string) string { return "" },
			inContent:   "",
			wantContent: "",
			wantSame:    true,
		},
		{
			name:        "transform collapses newlines",
			transform:   func(s string) string { return strings.ReplaceAll(s, "\n", " ") },
			inContent:   "a\nb",
			wantContent: "a b",
			wantSame:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewInputDialog("id", "Title", "Placeholder")
			d.SetInputTransform(tt.transform)

			in := tea.PasteMsg{Content: tt.inContent}
			out := d.transformInputMsg(in)

			paste, ok := out.(tea.PasteMsg)
			if !ok {
				t.Fatalf("expected PasteMsg back, got %T", out)
			}
			if paste.Content != tt.wantContent {
				t.Fatalf("transformed content = %q, want %q", paste.Content, tt.wantContent)
			}
			if tt.wantSame && out != tea.Msg(in) {
				t.Fatalf("expected original paste msg to pass through unchanged")
			}
		})
	}
}

func TestDialogTransformInputMsgPassthroughOtherTypes(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")
	d.SetInputTransform(func(string) string { return "X" })

	// A message type the transform does not handle must be returned unchanged
	// without ever invoking the transform (which would otherwise rewrite text).
	in := tea.WindowSizeMsg{Width: 10, Height: 20}
	out := d.transformInputMsg(in)

	win, ok := out.(tea.WindowSizeMsg)
	if !ok {
		t.Fatalf("expected WindowSizeMsg back, got %T", out)
	}
	if win != in {
		t.Fatalf("WindowSizeMsg was modified: got %+v want %+v", win, in)
	}
}

func TestDialogHide(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")
	d.Show()
	if !d.Visible() {
		t.Fatalf("expected dialog to be visible after Show")
	}

	d.Hide()
	if d.Visible() {
		t.Fatalf("expected dialog to be hidden after Hide")
	}

	// Hide is idempotent.
	d.Hide()
	if d.Visible() {
		t.Fatalf("expected dialog to remain hidden after second Hide")
	}
}

func TestDialogVisible(t *testing.T) {
	d := NewConfirmDialog("id", "Title", "Message")

	if d.Visible() {
		t.Fatalf("expected fresh dialog to be hidden")
	}

	d.Show()
	if !d.Visible() {
		t.Fatalf("expected dialog to be visible after Show")
	}

	d.Hide()
	if d.Visible() {
		t.Fatalf("expected dialog to be hidden after Hide")
	}
}

func TestDialogSetShowKeymapHints(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")

	// Default is false (zero value).
	if d.showKeymapHints {
		t.Fatalf("expected showKeymapHints to default to false")
	}

	d.SetShowKeymapHints(true)
	if !d.showKeymapHints {
		t.Fatalf("expected showKeymapHints to be true after enabling")
	}

	d.SetShowKeymapHints(false)
	if d.showKeymapHints {
		t.Fatalf("expected showKeymapHints to be false after disabling")
	}
}
