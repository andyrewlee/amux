package app

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
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
}

// HarnessMode values.
const (
	HarnessMonitor = "monitor"
	HarnessCenter  = "center"
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

	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}
	cfg.UI.ShowKeymapHints = opts.ShowKeymapHints

	switch opts.Mode {
	case "", HarnessMonitor:
		return newMonitorHarness(cfg, opts), nil
	case HarnessCenter:
		return newCenterHarness(cfg, opts), nil
	default:
		return nil, fmt.Errorf("unknown mode %q", opts.Mode)
	}
}

func newMonitorHarness(cfg *config.Config, opts HarnessOptions) *Harness {
	centerModel := center.New(cfg)
	centerModel.SetShowKeymapHints(opts.ShowKeymapHints)

	tabs := make([]*center.Tab, 0, opts.Tabs)
	for i := 0; i < opts.Tabs; i++ {
		wt := &data.Worktree{
			Name: fmt.Sprintf("wt-%d", i),
			Repo: fmt.Sprintf("/repo/%d", i),
			Root: fmt.Sprintf("/repo/%d/wt", i),
		}
		term := vterm.New(80, 24)
		tab := &center.Tab{
			ID:        center.TabID(fmt.Sprintf("tab-%d", i)),
			Name:      fmt.Sprintf("amp-%d", i),
			Assistant: "amp",
			Worktree:  wt,
			Terminal:  term,
			Running:   true,
		}
		centerModel.AddTab(tab)
		tabs = append(tabs, tab)
	}

	app := &App{
		config:          cfg,
		center:          centerModel,
		styles:          common.DefaultStyles(),
		width:           opts.Width,
		height:          opts.Height,
		helpOverlay:     common.NewHelpOverlay(),
		toast:           common.NewToastModel(),
		dashboardChrome: &compositor.ChromeCache{},
		centerChrome:    &compositor.ChromeCache{},
		sidebarChrome:   &compositor.ChromeCache{},
	}

	app.projects = make([]data.Project, 0, opts.Tabs)
	for i := 0; i < opts.Tabs; i++ {
		app.projects = append(app.projects, data.Project{
			Name: fmt.Sprintf("repo-%d", i),
			Path: fmt.Sprintf("/repo/%d", i),
		})
	}

	return &Harness{
		app:          app,
		mode:         HarnessMonitor,
		tabs:         tabs,
		hotTabs:      opts.HotTabs,
		payloadBytes: opts.PayloadBytes,
		newlineEvery: opts.NewlineEvery,
		payloadBuf:   make([]byte, 0, opts.PayloadBytes+32),
		spinner:      []byte{'|', '/', '-', '\\'},
	}
}

func newCenterHarness(cfg *config.Config, opts HarnessOptions) *Harness {
	centerModel := center.New(cfg)
	centerModel.SetShowKeymapHints(opts.ShowKeymapHints)

	dash := dashboard.New()
	dash.SetShowKeymapHints(opts.ShowKeymapHints)
	side := sidebar.New()
	side.SetShowKeymapHints(opts.ShowKeymapHints)
	sideTerm := sidebar.NewTerminalModel()
	sideTerm.SetShowKeymapHints(opts.ShowKeymapHints)

	layoutMgr := layout.NewManager()
	layoutMgr.Resize(opts.Width, opts.Height)

	wt := &data.Worktree{
		Name: "primary",
		Repo: "/repo/primary",
		Root: "/repo/primary/wt",
	}
	project := data.Project{Name: "primary", Path: wt.Repo}

	tabs := make([]*center.Tab, 0, opts.Tabs)
	for i := 0; i < opts.Tabs; i++ {
		term := vterm.New(80, 24)
		tab := &center.Tab{
			ID:        center.TabID(fmt.Sprintf("tab-%d", i)),
			Name:      fmt.Sprintf("amp-%d", i),
			Assistant: "amp",
			Worktree:  wt,
			Terminal:  term,
			Running:   true,
		}
		centerModel.AddTab(tab)
		tabs = append(tabs, tab)
	}
	centerModel.SetWorktree(wt)

	dash.SetProjects([]data.Project{project})

	app := &App{
		config:          cfg,
		layout:          layoutMgr,
		dashboard:       dash,
		center:          centerModel,
		sidebar:         side,
		sidebarTerminal: sideTerm,
		styles:          common.DefaultStyles(),
		width:           opts.Width,
		height:          opts.Height,
		helpOverlay:     common.NewHelpOverlay(),
		toast:           common.NewToastModel(),
		focusedPane:     messages.PaneCenter,
		dashboardChrome: &compositor.ChromeCache{},
		centerChrome:    &compositor.ChromeCache{},
		sidebarChrome:   &compositor.ChromeCache{},
	}

	app.layout.Resize(opts.Width, opts.Height)
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

// Step simulates output for the active tabs.
func (h *Harness) Step(frame int) {
	if h == nil || h.hotTabs == 0 {
		return
	}
	payload := h.buildPayload(frame)
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
	switch h.mode {
	case HarnessCenter:
		return h.app.viewLayerBased()
	default:
		return h.app.viewMonitorMode()
	}
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
