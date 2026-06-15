package app

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/vterm"
)

// HarnessOptions configures the headless UI harness.
type HarnessOptions struct {
	Mode            string
	Tabs            int
	Width           int
	Height          int
	HotTabs         int
	PayloadBytes    int
	NewlineEvery    int
	ShowKeymapHints bool
	// Overlay, when non-empty, puts the App into the corresponding overlay
	// state (dialog/settings/prefix/...) after the base pane is built, so the
	// rendered frame exercises composeOverlays instead of only base-pane chrome.
	// See applyHarnessOverlay in harness_overlay.go for the accepted values.
	Overlay string
}

// HarnessMode values.
const (
	HarnessCenter  = "center"
	HarnessSidebar = "sidebar"
	HarnessMonitor = "monitor"
)

// Harness drives a headless render loop for profiling.
type Harness struct {
	app *App

	mode         string
	tabs         []*center.Tab
	hotTabs      int
	payloadBytes int
	newlineEvery int
	payloadBuf   []byte
	spinner      []byte
	sidebarTerm  *sidebar.TerminalModel
}

// NewHarness builds a headless UI harness for the requested mode.
func NewHarness(opts HarnessOptions) (*Harness, error) {
	if opts.Tabs <= 0 {
		opts.Tabs = 1
	}
	if opts.Width <= 0 {
		opts.Width = 160
	}
	if opts.Height <= 0 {
		opts.Height = 48
	}
	if opts.HotTabs < 0 {
		opts.HotTabs = 0
	}
	if opts.PayloadBytes <= 0 {
		opts.PayloadBytes = 64
	}
	if err := validateHarnessOverlay(opts.Overlay); err != nil {
		return nil, err
	}

	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}
	cfg.UI.ShowKeymapHints = opts.ShowKeymapHints

	switch opts.Mode {
	case "", HarnessCenter:
		return newCenterHarness(cfg, opts), nil
	case HarnessSidebar:
		return newSidebarHarness(cfg, opts), nil
	case HarnessMonitor:
		return newMonitorHarness(cfg, opts), nil
	default:
		return nil, fmt.Errorf("unknown mode %q", opts.Mode)
	}
}

func newMonitorHarness(cfg *config.Config, opts HarnessOptions) *Harness {
	h := newSidebarHarness(cfg, opts)
	h.mode = HarnessMonitor
	return h
}

// newHarnessApp builds a services-off App through the real constructor path
// (newAppShell), so the harness exercises the same component construction and
// wiring order as the production app, then applies harness geometry/focus.
func newHarnessApp(cfg *config.Config, opts HarnessOptions, focused messages.PaneType) *App {
	app := newAppShell(cfg)
	app.setKeymapHintsEnabled(opts.ShowKeymapHints)
	app.width = opts.Width
	app.height = opts.Height
	app.focusedPane = focused
	app.layout.Resize(opts.Width, opts.Height)
	applyHarnessOverlay(app, opts.Overlay)
	return app
}

func harnessWorkspace() *data.Workspace {
	return &data.Workspace{
		Name: "primary",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
	}
}

func newCenterHarness(cfg *config.Config, opts HarnessOptions) *Harness {
	app := newHarnessApp(cfg, opts, messages.PaneCenter)

	ws := harnessWorkspace()
	project := data.Project{Name: "primary", Path: ws.Repo}

	tabs := make([]*center.Tab, 0, opts.Tabs)
	for i := 0; i < opts.Tabs; i++ {
		term := vterm.New(80, 24)
		tab := &center.Tab{
			ID:        center.TabID(fmt.Sprintf("tab-%d", i)),
			Name:      fmt.Sprintf("amp-%d", i),
			Assistant: "amp",
			Workspace: ws,
			Terminal:  term,
			Running:   true,
		}
		app.center.AddTab(tab)
		tabs = append(tabs, tab)
	}
	app.center.SetWorkspace(ws)

	app.dashboard.SetProjects([]data.Project{project})

	app.updateLayout()

	return &Harness{
		app:          app,
		mode:         HarnessCenter,
		tabs:         tabs,
		hotTabs:      opts.HotTabs,
		payloadBytes: opts.PayloadBytes,
		newlineEvery: opts.NewlineEvery,
		payloadBuf:   make([]byte, 0, opts.PayloadBytes+32),
		spinner:      []byte{'|', '/', '-', '\\'},
	}
}

func newSidebarHarness(cfg *config.Config, opts HarnessOptions) *Harness {
	app := newHarnessApp(cfg, opts, messages.PaneSidebarTerminal)

	ws := harnessWorkspace()
	project := data.Project{Name: "primary", Path: ws.Repo}

	app.dashboard.SetProjects([]data.Project{project})

	app.updateLayout()

	app.sidebarTerminal.AddTerminalForHarness(ws)

	return &Harness{
		app:          app,
		mode:         HarnessSidebar,
		hotTabs:      opts.HotTabs,
		payloadBytes: opts.PayloadBytes,
		newlineEvery: opts.NewlineEvery,
		payloadBuf:   make([]byte, 0, opts.PayloadBytes+32),
		spinner:      []byte{'|', '/', '-', '\\'},
		sidebarTerm:  app.sidebarTerminal,
	}
}

// Step simulates output for the active tabs.
func (h *Harness) Step(frame int) {
	if h == nil || h.hotTabs == 0 {
		return
	}
	payload := h.buildPayload(frame)
	if h.mode == HarnessSidebar || h.mode == HarnessMonitor {
		if h.sidebarTerm != nil {
			for i := 0; i < h.hotTabs; i++ {
				h.sidebarTerm.WriteToTerminal(payload)
			}
		}
		return
	}
	for i := 0; i < h.hotTabs && i < len(h.tabs); i++ {
		tab := h.tabs[i]
		if tab == nil {
			continue
		}
		tab.WriteToTerminal(payload)
	}
}

// Render returns the composed view for the harness mode.
func (h *Harness) Render() tea.View {
	if h == nil || h.app == nil {
		return tea.View{}
	}
	// Harness rendering bypasses App.Update, so synchronize pane focus flags
	// before drawing to match runtime focus/cursor behavior.
	h.app.syncPaneFocusFlags()
	return h.app.viewLayerBased()
}

func (h *Harness) buildPayload(frame int) []byte {
	if h.payloadBytes > cap(h.payloadBuf) {
		h.payloadBuf = make([]byte, 0, h.payloadBytes+32)
	}
	buf := h.payloadBuf[:0]
	buf = append(buf, '\r', 'f', 'r', 'a', 'm', 'e', ' ')
	buf = strconv.AppendInt(buf, int64(frame), 10)
	buf = append(buf, ' ')
	if len(h.spinner) > 0 {
		buf = append(buf, h.spinner[frame%len(h.spinner)])
	}
	for len(buf) < h.payloadBytes {
		buf = append(buf, 'x')
	}
	if h.newlineEvery > 0 && frame%h.newlineEvery == 0 {
		buf = append(buf, '\n')
	}
	h.payloadBuf = buf
	return buf
}
