package app

import "testing"

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
