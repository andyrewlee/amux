package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/layout"
)

// newLayout builds a sized layout manager for view-content tests. The geometry
// drives the pure width/height math in app_view_content.go without needing a
// live Bubble Tea program.
func newLayout(w, h int) *layout.Manager {
	m := layout.NewManager()
	m.Resize(w, h)
	return m
}

func TestCenterPaneStyle_DimensionsAndFrame(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{name: "three pane wide terminal", width: 200, height: 60},
		{name: "two pane medium terminal", width: 110, height: 40},
		{name: "one pane narrow terminal", width: 40, height: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{layout: newLayout(tt.width, tt.height)}

			style := a.centerPaneStyle()

			// The style is built as Width(CenterWidth-2)/Height(Height-2).
			// lipgloss clamps negative dimensions to 0, so the expected value is
			// the production input floored at 0 (matters in one-pane mode where
			// CenterWidth is 0 and CenterWidth-2 is negative).
			wantWidth := max(a.layout.CenterWidth()-2, 0)
			wantHeight := max(a.layout.Height()-2, 0)
			if got := style.GetWidth(); got != wantWidth {
				t.Fatalf("width = %d, want %d (CenterWidth-2 floored at 0)", got, wantWidth)
			}
			if got := style.GetHeight(); got != wantHeight {
				t.Fatalf("height = %d, want %d (Height-2 floored at 0)", got, wantHeight)
			}
			// The style adds a rounded border (1 cell each side) plus 1 cell of
			// horizontal padding: frameX = 4, frameY = 2 regardless of geometry.
			frameX, frameY := style.GetFrameSize()
			if frameX != 4 {
				t.Fatalf("frameX = %d, want 4 (border + horizontal padding)", frameX)
			}
			if frameY != 2 {
				t.Fatalf("frameY = %d, want 2 (top+bottom border)", frameY)
			}
		})
	}
}

func TestCenterPaneContentOrigin_NilLayoutReturnsZero(t *testing.T) {
	a := &App{}

	x, y := a.centerPaneContentOrigin()
	if x != 0 || y != 0 {
		t.Fatalf("origin with nil layout = (%d,%d), want (0,0)", x, y)
	}
}

func TestCenterPaneContentOrigin_AccountsForGapWhenCenterShown(t *testing.T) {
	// A wide terminal lands in three-pane mode, so ShowCenter() is true and the
	// gap between dashboard and center must be folded into the X origin.
	a := &App{layout: newLayout(200, 60)}
	if !a.layout.ShowCenter() {
		t.Fatal("test setup expects a layout that shows the center pane")
	}

	x, y := a.centerPaneContentOrigin()

	// frameX/2 = 2, frameY/2 = 1 for the rounded border + padding style.
	wantX := a.layout.LeftGutter() + a.layout.DashboardWidth() + a.layout.GapX() + 2
	wantY := a.layout.TopGutter() + 1
	if x != wantX {
		t.Fatalf("x = %d, want %d", x, wantX)
	}
	if y != wantY {
		t.Fatalf("y = %d, want %d", y, wantY)
	}
}

func TestCenterPaneContentOrigin_OmitsGapWhenCenterHidden(t *testing.T) {
	// A narrow terminal collapses to one-pane mode where ShowCenter() is false,
	// so GapX() must NOT be added to the X origin.
	a := &App{layout: newLayout(40, 20)}
	if a.layout.ShowCenter() {
		t.Fatal("test setup expects a one-pane layout that hides the center pane")
	}

	x, _ := a.centerPaneContentOrigin()

	wantX := a.layout.LeftGutter() + a.layout.DashboardWidth() + 2 // no gap, frameX/2=2
	if x != wantX {
		t.Fatalf("x = %d, want %d (gap excluded when center hidden)", x, wantX)
	}
}

func TestRenderCenterPaneContent_Routing(t *testing.T) {
	base := func() *App {
		return &App{
			layout: newLayout(200, 60),
			styles: common.DefaultStyles(),
			config: &config.Config{},
		}
	}

	t.Run("welcome screen takes precedence", func(t *testing.T) {
		a := base()
		a.showWelcome = true
		a.activeWorkspace = &data.Workspace{Name: "ignored"}

		out := ansi.Strip(a.renderCenterPaneContent())
		if !strings.Contains(out, "Add project") {
			t.Fatalf("expected welcome content, got: %q", out)
		}
	})

	t.Run("active workspace renders workspace info", func(t *testing.T) {
		a := base()
		a.activeWorkspace = &data.Workspace{Name: "feature-x", Branch: "feat", Root: "/tmp/ws"}

		out := ansi.Strip(a.renderCenterPaneContent())
		if !strings.Contains(out, "feature-x") {
			t.Fatalf("expected workspace name in output, got: %q", out)
		}
		if !strings.Contains(out, "New agent") {
			t.Fatalf("expected new-agent button in workspace info, got: %q", out)
		}
	})

	t.Run("no welcome and no workspace shows prompt", func(t *testing.T) {
		a := base()

		out := a.renderCenterPaneContent()
		if out != "Select a workspace from the dashboard" {
			t.Fatalf("unexpected fallback content: %q", out)
		}
	})
}

func TestRenderWorkspaceInfo_IncludesCoreFields(t *testing.T) {
	a := &App{
		layout: newLayout(200, 60),
		styles: common.DefaultStyles(),
		config: &config.Config{},
		activeWorkspace: &data.Workspace{
			Name:   "feature-x",
			Branch: "feat/foo",
			Root:   "/tmp/amux/feature-x",
		},
	}

	out := ansi.Strip(a.renderWorkspaceInfo())

	for _, want := range []string{"feature-x", "Branch: feat/foo", "Path: /tmp/amux/feature-x", "[New agent]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace info missing %q, got:\n%s", want, out)
		}
	}
	// No active project => no Project line.
	if strings.Contains(out, "Project:") {
		t.Fatalf("did not expect a project line without an active project, got:\n%s", out)
	}
}

func TestRenderWorkspaceInfo_IncludesProjectWhenSet(t *testing.T) {
	a := &App{
		layout:          newLayout(200, 60),
		styles:          common.DefaultStyles(),
		config:          &config.Config{},
		activeWorkspace: &data.Workspace{Name: "ws", Branch: "b", Root: "/r"},
		activeProject:   &data.Project{Name: "my-project"},
	}

	out := ansi.Strip(a.renderWorkspaceInfo())
	if !strings.Contains(out, "Project: my-project") {
		t.Fatalf("expected project line, got:\n%s", out)
	}
}

func TestRenderWorkspaceInfo_KeymapHintTogglesWithConfig(t *testing.T) {
	const hint = "C-Spc t a:new agent"

	mk := func(showHints bool) string {
		a := &App{
			layout:          newLayout(200, 60),
			styles:          common.DefaultStyles(),
			config:          &config.Config{UI: config.UISettings{ShowKeymapHints: showHints}},
			activeWorkspace: &data.Workspace{Name: "ws", Branch: "b", Root: "/r"},
		}
		return ansi.Strip(a.renderWorkspaceInfo())
	}

	if got := mk(true); !strings.Contains(got, hint) {
		t.Fatalf("expected keymap hint when enabled, got:\n%s", got)
	}
	if got := mk(false); strings.Contains(got, hint) {
		t.Fatalf("did not expect keymap hint when disabled, got:\n%s", got)
	}
}

func TestRenderWorkspaceInfo_NewAgentButtonFocusHighlight(t *testing.T) {
	// The active style is bold; the inactive style is not. We assert the focus
	// flag changes the rendered escape sequence for the button. Comparing raw
	// (non-stripped) output captures the styling difference.
	mk := func(focused bool, idx int) string {
		a := &App{
			layout:           newLayout(200, 60),
			styles:           common.DefaultStyles(),
			config:           &config.Config{},
			activeWorkspace:  &data.Workspace{Name: "ws", Branch: "b", Root: "/r"},
			centerBtnFocused: focused,
			centerBtnIndex:   idx,
		}
		return a.renderWorkspaceInfo()
	}

	unfocused := mk(false, 0)
	focused := mk(true, 0)
	wrongIndex := mk(true, 1)

	// Stripped text is identical regardless of focus.
	if ansi.Strip(unfocused) == "" || !strings.Contains(ansi.Strip(focused), "[New agent]") {
		t.Fatalf("expected [New agent] label present, got: %q", ansi.Strip(focused))
	}
	// Focus must change the styled output (bold active style applied).
	if focused == unfocused {
		t.Fatal("expected focused button to render with different styling than unfocused")
	}
	// Focusing the wrong index (1, which has no button) must not apply the
	// active style, so it should match the unfocused rendering.
	if wrongIndex != unfocused {
		t.Fatal("expected non-zero focus index to leave the single button inactive")
	}
}

func TestRenderWelcome_CentersAndContainsButtons(t *testing.T) {
	a := &App{
		layout: newLayout(200, 60),
		styles: common.DefaultStyles(),
		config: &config.Config{},
	}

	out := a.renderWelcome()
	stripped := ansi.Strip(out)

	for _, want := range []string{"[Add project]", "[Settings]"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("welcome missing %q, got:\n%s", want, stripped)
		}
	}

	// lipgloss.Place pads the content to the requested box. The widest line must
	// not exceed CenterWidth-4, and the block must have multiple rows.
	wantWidth := a.layout.CenterWidth() - 4
	lines := strings.Split(stripped, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected centered multi-line welcome, got %d lines", len(lines))
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > wantWidth {
			t.Fatalf("line width %d exceeds placement width %d: %q", w, wantWidth, line)
		}
	}
}

func TestWelcomeContent_ButtonsAndLogo(t *testing.T) {
	a := &App{styles: common.DefaultStyles(), config: &config.Config{}}

	out := ansi.Strip(a.welcomeContent())

	for _, want := range []string{"[Add project]", "[Settings]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("welcome content missing %q, got:\n%s", want, out)
		}
	}
	// The logo art (rendered from welcomeLogo) contributes the "8888b." line.
	if !strings.Contains(out, "8888b.") {
		t.Fatalf("expected logo art in welcome content, got:\n%s", out)
	}
}

func TestWelcomeContent_KeymapHintTogglesWithConfig(t *testing.T) {
	const hint = "Dashboard: j/k to move"

	mk := func(showHints bool) string {
		a := &App{
			styles: common.DefaultStyles(),
			config: &config.Config{UI: config.UISettings{ShowKeymapHints: showHints}},
		}
		return ansi.Strip(a.welcomeContent())
	}

	if got := mk(true); !strings.Contains(got, hint) {
		t.Fatalf("expected dashboard hint when enabled, got:\n%s", got)
	}
	if got := mk(false); strings.Contains(got, hint) {
		t.Fatalf("did not expect dashboard hint when disabled, got:\n%s", got)
	}
}

func TestWelcomeContent_ButtonFocusHighlight(t *testing.T) {
	mk := func(focused bool, idx int) string {
		a := &App{
			styles:           common.DefaultStyles(),
			config:           &config.Config{},
			centerBtnFocused: focused,
			centerBtnIndex:   idx,
		}
		return a.welcomeContent()
	}

	unfocused := mk(false, 0)
	addProjectFocused := mk(true, 0)
	settingsFocused := mk(true, 1)
	outOfRangeFocused := mk(true, 2)

	// Both buttons are always present in the stripped text.
	if !strings.Contains(ansi.Strip(unfocused), "[Add project]") ||
		!strings.Contains(ansi.Strip(unfocused), "[Settings]") {
		t.Fatalf("expected both buttons present, got: %q", ansi.Strip(unfocused))
	}

	// Each valid focus index must change the styled output.
	if addProjectFocused == unfocused {
		t.Fatal("expected focusing Add project (index 0) to change styling")
	}
	if settingsFocused == unfocused {
		t.Fatal("expected focusing Settings (index 1) to change styling")
	}
	// Focusing index 0 vs 1 must produce distinct renderings.
	if addProjectFocused == settingsFocused {
		t.Fatal("expected Add project focus to differ from Settings focus")
	}
	// An out-of-range index leaves both buttons inactive => same as unfocused.
	if outOfRangeFocused != unfocused {
		t.Fatal("expected out-of-range focus index to leave both buttons inactive")
	}
}

func TestWelcomeLogo_ReturnsArtAndBoldPrimaryStyle(t *testing.T) {
	a := &App{}

	logo, style := a.welcomeLogo()

	if !strings.Contains(logo, "8888b.") {
		t.Fatalf("expected ascii logo art, got: %q", logo)
	}
	// The logo is a multi-line block.
	if n := strings.Count(logo, "\n"); n < 4 {
		t.Fatalf("expected multi-line logo (>=5 lines), got %d newlines", n)
	}
	if !style.GetBold() {
		t.Fatal("expected logo style to be bold")
	}
	if style.GetForeground() != common.ColorPrimary() {
		t.Fatalf("logo style foreground = %v, want ColorPrimary", style.GetForeground())
	}
	// Rendering through the style must not drop the underlying glyphs.
	if rendered := ansi.Strip(style.Render(logo)); !strings.Contains(rendered, "8888b.") {
		t.Fatalf("styled logo lost its art: %q", rendered)
	}
}
