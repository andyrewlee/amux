package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestRenderChoiceColumns_RespectsSeparatorGutterInFitLoop(t *testing.T) {
	app := &App{}
	choices := []prefixPaletteChoice{
		{Key: "a", Desc: "first"},
		{Key: "b", Desc: "second"},
		{Key: "c", Desc: "third"},
	}

	lines := app.renderChoiceColumns(choices, 64)
	if len(lines) != 2 {
		t.Fatalf("expected 2 rows (2 columns) at content width 64, got %d", len(lines))
	}
}

func TestRenderPrefixPalette_RootSectionsShareHeaderRow(t *testing.T) {
	app := &App{
		prefixActive: true,
		width:        120,
		height:       24,
		styles:       common.DefaultStyles(),
	}

	lines := strings.Split(ansi.Strip(app.renderPrefixPalette()), "\n")
	for _, line := range lines {
		if strings.Contains(line, "General") {
			if !strings.Contains(line, "Tabs") {
				t.Fatalf("expected root section headers on one row, got: %q", line)
			}
			return
		}
	}

	t.Fatal("expected to find a row containing root section headers")
}

func TestRenderPrefixPalette_RootSectionsShareFirstCommandRow(t *testing.T) {
	app := &App{
		prefixActive: true,
		width:        120,
		height:       24,
		styles:       common.DefaultStyles(),
	}

	lines := strings.Split(ansi.Strip(app.renderPrefixPalette()), "\n")
	for _, line := range lines {
		if strings.Contains(line, "add project") {
			if !strings.Contains(line, "tab actions") {
				t.Fatalf("expected first command row to include all root sections, got: %q", line)
			}
			return
		}
	}

	t.Fatal("expected to find a row containing root section commands")
}

func TestRenderPrefixPalette_FiltersUnavailableRootCommands(t *testing.T) {
	h := newCenterHarness(nil, HarnessOptions{
		Width:  120,
		Height: 24,
		Tabs:   1,
	})
	app := h.app
	app.prefixActive = true

	content := ansi.Strip(app.renderPrefixPalette())
	if strings.Contains(content, "1-9") {
		t.Fatalf("expected numeric jump to be hidden with <=1 tab, got:\n%s", content)
	}
	// Pane-focus commands should be present when navigation is possible.
	// From PaneCenter, "focus left" should be visible (dashboard exists).
	if !strings.Contains(content, "focus left") {
		t.Fatalf("expected 'focus left' in palette from PaneCenter, got:\n%s", content)
	}
}

func TestRenderPrefixPalette_HidesFocusLeftFromDashboard(t *testing.T) {
	h := newCenterHarness(nil, HarnessOptions{
		Width:  120,
		Height: 24,
		Tabs:   1,
	})
	app := h.app
	app.prefixActive = true
	app.focusedPane = messages.PaneDashboard

	content := ansi.Strip(app.renderPrefixPalette())
	if strings.Contains(content, "focus left") {
		t.Fatalf("expected 'focus left' hidden from dashboard, got:\n%s", content)
	}
}

func TestNextPrefixPaletteChoices_FiltersUnavailableSubcommands(t *testing.T) {
	h := newCenterHarness(nil, HarnessOptions{
		Width:  120,
		Height: 24,
		Tabs:   0,
	})
	app := h.app
	app.prefixSequence = []string{"t"}

	choices := app.nextPrefixPaletteChoices()
	if len(choices) != 0 {
		t.Fatalf("expected unavailable tab subcommands to be hidden, got %+v", choices)
	}
}
