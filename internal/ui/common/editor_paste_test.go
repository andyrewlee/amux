package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Editor-paste tests drive real tea.PasteMsg values through each custom
// editor's Update — no synthesized KeyPressMsg — and assert exact stored text,
// first-line policy, no command emission, and no structural side effects.

func TestEditorPasteEnvExistingValue(t *testing.T) {
	d := NewEnvDialog(map[string]string{"PATH": "/bin", "AAA": "x"})
	d.Show()
	d.cursor = 1 // rows are sorted: PATH after AAA

	// Paste with j/k, a space, and a newline: text lands in the focused
	// value, never as navigation or submission.
	d2, cmd := d.Update(tea.PasteMsg{Content: ":/opt jk\nSECOND LINE"})
	if cmd != nil {
		t.Fatal("paste returned a command")
	}
	if got := d2.Env()["PATH"]; got != "/bin:/opt jk" {
		t.Fatalf("PATH = %q, want %q", got, "/bin:/opt jk")
	}
	if got := d2.Env()["AAA"]; got != "x" {
		t.Fatalf("AAA changed: %q", got)
	}
	if !d2.Visible() {
		t.Fatal("paste closed the dialog")
	}
}

func TestEditorPasteEnvAddModeFields(t *testing.T) {
	d := NewEnvDialog(map[string]string{})
	d.Show()
	d.startAdd()

	if _, cmd := d.Update(tea.PasteMsg{Content: "MY_VAR\tX"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if d.addName != "MY_VARX" {
		t.Fatalf("addName = %q, want %q (tab dropped)", d.addName, "MY_VARX")
	}
	d.addField = 1
	if _, cmd := d.Update(tea.PasteMsg{Content: "va lue\r\nignored"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if d.addValue != "va lue" {
		t.Fatalf("addValue = %q, want %q (first line only)", d.addValue, "va lue")
	}
	if !d.adding {
		t.Fatal("paste closed add mode")
	}
}

func TestEditorPasteEnvHiddenNoop(t *testing.T) {
	d := NewEnvDialog(map[string]string{"K": "v"})
	// not Shown
	d2, cmd := d.Update(tea.PasteMsg{Content: "pasted"})
	if cmd != nil {
		t.Fatal("hidden editor returned a command")
	}
	if got := d2.Env()["K"]; got != "v" {
		t.Fatalf("hidden editor mutated: %q", got)
	}
}

func TestEditorPasteScriptsCommandFields(t *testing.T) {
	d := NewScriptsDialog("", "", "", "", ScriptsModeConcurrent)
	d.Show()
	fields := []struct {
		cursor int
		get    func() string
		want   string
	}{
		{0, func() string { s, _, _, _, _ := d.Values(); return s }, "make setup j"},
		{1, func() string { _, r, _, _, _ := d.Values(); return r }, "make run"},
		{2, func() string { _, _, a, _, _ := d.Values(); return a }, "make archive"},
		{3, func() string { _, _, _, o, _ := d.Values(); return o }, "make done"},
	}
	for _, f := range fields {
		d.cursor = f.cursor
		if _, cmd := d.Update(tea.PasteMsg{Content: f.want + "\nsecond"}); cmd != nil {
			t.Fatalf("paste on field %d returned a command", f.cursor)
		}
		if got := f.get(); got != f.want {
			t.Fatalf("field %d = %q, want %q", f.cursor, got, f.want)
		}
	}
}

func TestEditorPasteScriptsModeRowIgnoresPaste(t *testing.T) {
	d := NewScriptsDialog("", "", "", "", ScriptsModeConcurrent)
	d.Show()
	d.cursor = 4 // mode row is a toggle

	before := func() string { _, _, _, _, m := d.Values(); return m }()
	if _, cmd := d.Update(tea.PasteMsg{Content: " \r\n"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if got := func() string { _, _, _, _, m := d.Values(); return m }(); got != before {
		t.Fatalf("paste toggled mode row: %q -> %q", before, got)
	}
}

func TestEditorPasteScriptsHiddenNoop(t *testing.T) {
	d := NewScriptsDialog("orig", "", "", "", ScriptsModeConcurrent)
	if _, cmd := d.Update(tea.PasteMsg{Content: "pasted"}); cmd != nil {
		t.Fatal("hidden editor returned a command")
	}
	if s, _, _, _, _ := d.Values(); s != "orig" {
		t.Fatalf("hidden editor mutated: %q", s)
	}
}

func TestEditorPasteSettingsTmuxFields(t *testing.T) {
	s := NewSettingsDialog(ThemeGruvbox, "srv", "/cfg", "1s")
	s.Show()

	cases := []struct {
		item settingsItem
		in   string
		want func() string
	}{
		{settingsItemTmuxServer, "server- x\nmore", func() string { return s.tmuxServer }},
		{settingsItemTmuxConfig, "/tmp/cfg j\nmore", func() string { return s.tmuxConfigPath }},
		{settingsItemTmuxSync, "5s\nmore", func() string { return s.tmuxSyncInterval }},
	}
	wants := []string{"srvserver- x", "/cfg/tmp/cfg j", "1s5s"}
	for i, c := range cases {
		s.focusedItem = c.item
		if _, cmd := s.Update(tea.PasteMsg{Content: c.in}); cmd != nil {
			t.Fatalf("paste on item %d returned a command", c.item)
		}
		if got := c.want(); got != wants[i] {
			t.Fatalf("tmux field %d = %q, want %q", c.item, got, wants[i])
		}
	}
}

func TestEditorPasteSettingsDurationFilter(t *testing.T) {
	s := NewSettingsDialog(ThemeGruvbox, "", "", "")
	s.Show()
	s.focusedItem = settingsItemTmuxSync
	if _, cmd := s.Update(tea.PasteMsg{Content: "10xsabc"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if got := s.tmuxSyncInterval; got != "10s" {
		t.Fatalf("tmuxSyncInterval = %q, want duration-filtered %q", got, "10s")
	}
}

func TestEditorPasteSettingsAssistantFields(t *testing.T) {
	s := NewSettingsDialog(ThemeGruvbox, "", "", "")
	s.Show()
	s.SetAssistants([]string{"claude"}, map[string]string{"claude": "claude"})

	// Existing command edit.
	s.focusedItem = settingsItemAssistants
	if _, cmd := s.Update(tea.PasteMsg{Content: " --resume\nother"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if got := s.AssistantCommands()["claude"]; got != "claude --resume" {
		t.Fatalf("claude command = %q, want %q", got, "claude --resume")
	}

	// Add-mode fields.
	s.assistantAdding = true
	s.assistantAddField = 0
	if _, cmd := s.Update(tea.PasteMsg{Content: "mytool j"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if s.assistantAddName != "mytool j" {
		t.Fatalf("assistantAddName = %q, want %q", s.assistantAddName, "mytool j")
	}
	s.assistantAddField = 1
	if _, cmd := s.Update(tea.PasteMsg{Content: "mytool --interactive\nsecond"}); cmd != nil {
		t.Fatal("paste returned a command")
	}
	if s.assistantAddCmd != "mytool --interactive" {
		t.Fatalf("assistantAddCmd = %q, want %q", s.assistantAddCmd, "mytool --interactive")
	}
	if !s.assistantAdding {
		t.Fatal("paste closed add mode")
	}
}

func TestEditorPasteSettingsNonTextRowsIgnore(t *testing.T) {
	s := NewSettingsDialog(ThemeGruvbox, "", "", "")
	s.Show()
	for _, item := range []settingsItem{settingsItemTheme, settingsItemUpdate, settingsItemClose} {
		s.focusedItem = item
		before := s.View()
		if _, cmd := s.Update(tea.PasteMsg{Content: "pasted"}); cmd != nil {
			t.Fatalf("paste on item %d returned a command", item)
		}
		if s.View() != before {
			t.Fatalf("paste changed non-text row %d", item)
		}
	}
}

func TestEditorPasteSettingsHiddenNoop(t *testing.T) {
	s := NewSettingsDialog(ThemeGruvbox, "srv", "", "")
	s.focusedItem = settingsItemTmuxServer
	if _, cmd := s.Update(tea.PasteMsg{Content: "pasted"}); cmd != nil {
		t.Fatal("hidden editor returned a command")
	}
	if s.tmuxServer != "srv" {
		t.Fatalf("hidden editor mutated: %q", s.tmuxServer)
	}
}
