package sidebar

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	lazygitFlushQuiet       = 20 * time.Millisecond
	lazygitFlushMax         = 80 * time.Millisecond
	lazygitFlushChunkSize   = 32 * 1024
	lazygitReadBufferSize   = 32 * 1024
	lazygitReadQueueSize    = 32
	lazygitFrameInterval    = time.Second / 24
	lazygitMaxPendingBytes  = 256 * 1024
	lazygitMaxBufferedBytes = 4 * 1024 * 1024
)

// LazygitModel is the Bubbletea model for the lazygit sidebar pane.
type LazygitModel struct {
	workspace *data.Workspace
	focused   bool
	width     int
	height    int
	styles    common.Styles
	showKeymapHints bool

	msgSink func(tea.Msg)

	mu           sync.Mutex
	term         *pty.Terminal
	vt           *vterm.VTerm
	running      bool
	lastWidth    int
	lastHeight   int
	ptyNoiseTrailing []byte

	// PTY buffering
	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time

	// PTY reader state
	readerActive bool
	readerCancel chan struct{}
	ptyMsgCh     chan tea.Msg
	ptyHeartbeat int64
	runGen       uint64

	// Snapshot cache
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool
}

// NewLazygitModel creates a new lazygit model.
func NewLazygitModel() *LazygitModel {
	return &LazygitModel{
		styles: common.DefaultStyles(),
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *LazygitModel) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *LazygitModel) SetStyles(styles common.Styles) {
	m.styles = styles
}

// SetMsgSink sets a callback for PTY messages.
func (m *LazygitModel) SetMsgSink(sink func(tea.Msg)) {
	m.msgSink = sink
}

// Init initializes the lazygit model.
func (m *LazygitModel) Init() tea.Cmd {
	return nil
}

// Focus sets focus.
func (m *LazygitModel) Focus() {
	m.focused = true
	m.mu.Lock()
	if m.vt != nil {
		m.vt.ShowCursor = true
	}
	m.cachedSnap = nil
	m.cachedVersion = 0
	m.mu.Unlock()
}

// Blur removes focus.
func (m *LazygitModel) Blur() {
	m.focused = false
	m.mu.Lock()
	if m.vt != nil {
		m.vt.ShowCursor = false
	}
	m.cachedSnap = nil
	m.cachedVersion = 0
	m.mu.Unlock()
}

// Focused returns whether the pane is focused.
func (m *LazygitModel) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace and restarts lazygit if needed.
func (m *LazygitModel) SetWorkspace(ws *data.Workspace) tea.Cmd {
	if sameWorkspaceByCanonicalPaths(m.workspace, ws) {
		m.workspace = ws
		return nil
	}
	m.stopLazygit()
	m.workspace = ws
	if ws == nil {
		logging.Info("lazygit: workspace cleared")
		return nil
	}
	logging.Info("lazygit: starting for workspace=%s root=%s size=%dx%d", ws.ID(), ws.Root, m.width, m.height)
	return m.startLazygit()
}

// SetSize sets the component size.
func (m *LazygitModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	termWidth, termHeight := m.contentSize()
	m.mu.Lock()
	if m.vt != nil && (m.lastWidth != termWidth || m.lastHeight != termHeight) {
		m.lastWidth = termWidth
		m.lastHeight = termHeight
		m.vt.Resize(termWidth, termHeight)
		if m.term != nil {
			_ = m.term.SetSize(uint16(termHeight), uint16(termWidth))
		}
	}
	m.mu.Unlock()
}

// contentSize returns the usable terminal dimensions.
func (m *LazygitModel) contentSize() (int, int) {
	w := m.width
	h := m.height
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	return w, h
}

// startLazygit launches lazygit in a PTY.
func (m *LazygitModel) startLazygit() tea.Cmd {
	ws := m.workspace
	if ws == nil {
		return nil
	}
	termWidth, termHeight := m.contentSize()
	wsID := string(ws.ID())
	root := ws.Root

	m.runGen++
	gen := m.runGen

	return func() tea.Msg {
		env := []string{"COLORTERM=truecolor"}
		logging.Info("lazygit: launching PTY workspace=%s root=%s size=%dx%d gen=%d", wsID, root, termWidth, termHeight, gen)
		term, err := pty.NewWithSize("lazygit", root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			logging.Warn("lazygit: PTY launch failed: %v", err)
			return LazygitStarted{WorkspaceID: wsID, RunGen: gen, Err: err}
		}
		logging.Info("lazygit: PTY launched OK workspace=%s gen=%d", wsID, gen)
		return LazygitStarted{WorkspaceID: wsID, RunGen: gen, Terminal: term}
	}
}

// LazygitStarted is returned when lazygit starts (or fails to start).
type LazygitStarted struct {
	WorkspaceID string
	RunGen      uint64
	Terminal    *pty.Terminal
	Err         error
}

// handleStarted processes a LazygitStarted message.
func (m *LazygitModel) handleStarted(msg LazygitStarted) tea.Cmd {
	if msg.RunGen != m.runGen {
		// Stale - workspace or gen changed before lazygit started
		if msg.Terminal != nil {
			msg.Terminal.Close()
		}
		return nil
	}
	if msg.Err != nil {
		logging.Warn("lazygit failed to start: %v", msg.Err)
		return nil
	}

	termWidth, termHeight := m.contentSize()
	term := msg.Terminal

	vt := vterm.New(termWidth, termHeight)
	vt.AllowAltScreenScrollback = true
	vt.ShowCursor = m.focused
	vt.SetResponseWriter(func(data []byte) {
		if term != nil {
			if _, err := term.Write(data); err != nil {
				logging.Debug("lazygit vterm response write failed: %v", err)
			}
		}
	})

	if err := term.SetSize(uint16(termHeight), uint16(termWidth)); err != nil {
		logging.Debug("lazygit initial resize failed: %v", err)
	}

	m.mu.Lock()
	m.term = term
	m.vt = vt
	m.running = true
	m.lastWidth = termWidth
	m.lastHeight = termHeight
	m.pendingOutput = m.pendingOutput[:0]
	m.cachedSnap = nil
	m.cachedVersion = 0
	m.mu.Unlock()

	return m.startPTYReader(msg.WorkspaceID, msg.RunGen)
}

// stopLazygit stops the running lazygit PTY.
func (m *LazygitModel) stopLazygit() {
	m.stopPTYReader()
	m.mu.Lock()
	term := m.term
	m.term = nil
	m.running = false
	m.vt = nil
	m.pendingOutput = m.pendingOutput[:0]
	m.cachedSnap = nil
	m.cachedVersion = 0
	m.mu.Unlock()
	// Reset flush state so the next workspace's output gets a fresh flush schedule.
	m.flushScheduled = false
	m.flushPendingSince = time.Time{}
	if term != nil {
		term.Close()
	}
}

// startPTYReader starts the goroutine that reads from the lazygit PTY.
func (m *LazygitModel) startPTYReader(wsID string, gen uint64) tea.Cmd {
	m.mu.Lock()
	if m.readerActive {
		m.mu.Unlock()
		return nil
	}
	if m.term == nil || !m.running {
		m.mu.Unlock()
		return nil
	}
	if m.readerCancel != nil {
		common.SafeClose(m.readerCancel)
	}
	m.readerCancel = make(chan struct{})
	m.ptyMsgCh = make(chan tea.Msg, lazygitReadQueueSize)
	m.readerActive = true
	atomic.StoreInt64(&m.ptyHeartbeat, time.Now().UnixNano())
	term := m.term
	cancel := m.readerCancel
	msgCh := m.ptyMsgCh
	m.mu.Unlock()

	sink := m.msgSink

	safego.Go("sidebar.lazygit_reader", func() {
		defer m.markPTYReaderStopped()
		common.RunPTYReader(term, msgCh, cancel, &m.ptyHeartbeat, common.PTYReaderConfig{
			Label:           "sidebar.lazygit_read_loop",
			ReadBufferSize:  lazygitReadBufferSize,
			ReadQueueSize:   lazygitReadQueueSize,
			FrameInterval:   lazygitFrameInterval,
			MaxPendingBytes: lazygitMaxPendingBytes,
		}, common.PTYMsgFactory{
			Output: func(data []byte) tea.Msg {
				return messages.LazygitPTYOutput{WorkspaceID: wsID, RunGen: gen, Data: data}
			},
			Stopped: func(err error) tea.Msg {
				return messages.LazygitPTYStopped{WorkspaceID: wsID, RunGen: gen, Err: err}
			},
		})
	})
	safego.Go("sidebar.lazygit_forward", func() {
		m.forwardPTYMsgs(msgCh, sink)
	})
	return nil
}

func (m *LazygitModel) forwardPTYMsgs(msgCh <-chan tea.Msg, sink func(tea.Msg)) {
	for msg := range msgCh {
		if sink != nil {
			sink(msg)
		}
	}
}

func (m *LazygitModel) stopPTYReader() {
	m.mu.Lock()
	if m.readerCancel != nil {
		common.SafeClose(m.readerCancel)
		m.readerCancel = nil
	}
	m.readerActive = false
	m.ptyMsgCh = nil
	m.mu.Unlock()
	atomic.StoreInt64(&m.ptyHeartbeat, 0)
}

func (m *LazygitModel) markPTYReaderStopped() {
	m.mu.Lock()
	m.readerActive = false
	m.ptyMsgCh = nil
	m.mu.Unlock()
	atomic.StoreInt64(&m.ptyHeartbeat, 0)
}

// Update handles messages.
func (m *LazygitModel) Update(msg tea.Msg) (*LazygitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case LazygitStarted:
		return m, m.handleStarted(msg)

	case messages.LazygitPTYOutput:
		return m, m.handlePTYOutput(msg)

	case messages.LazygitPTYFlush:
		return m, m.handlePTYFlush(msg)

	case messages.LazygitPTYStopped:
		m.handlePTYStopped(msg)
		return m, nil

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}
		m.mu.Lock()
		term := m.term
		vt := m.vt
		if vt != nil && vt.IsScrolled() {
			vt.ScrollViewToBottom()
		}
		m.mu.Unlock()
		if term != nil {
			input := common.KeyToBytes(msg)
			if len(input) > 0 {
				if err := term.SendString(string(input)); err != nil {
					logging.Warn("lazygit input failed: %v", err)
				}
			}
		}
		return m, nil

	case tea.PasteMsg:
		if !m.focused {
			return m, nil
		}
		m.mu.Lock()
		term := m.term
		m.mu.Unlock()
		if term != nil {
			text := "\x1b[200~" + msg.Content + "\x1b[201~"
			if err := term.SendString(text); err != nil {
				logging.Warn("lazygit paste failed: %v", err)
			}
		}
		return m, nil

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		m.mu.Lock()
		if m.vt != nil {
			delta := common.ScrollDeltaForHeight(m.vt.Height, 8)
			if msg.Button == tea.MouseWheelUp {
				m.vt.ScrollView(delta)
			} else if msg.Button == tea.MouseWheelDown {
				m.vt.ScrollView(-delta)
			}
		}
		m.mu.Unlock()
		return m, nil

	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}
		m.mu.Lock()
		term := m.term
		m.mu.Unlock()
		if term != nil {
			// Forward mouse click as escape sequence
			btn := 0
			switch msg.Button {
			case tea.MouseRight:
				btn = 2
			case tea.MouseMiddle:
				btn = 1
			}
			seq := fmt.Sprintf("\x1b[M%c%c%c", byte(32+btn), byte(32+msg.X+1), byte(32+msg.Y+1))
			_ = term.SendString(seq)
		}
		return m, nil
	}
	return m, nil
}

func (m *LazygitModel) handlePTYOutput(msg messages.LazygitPTYOutput) tea.Cmd {
	if msg.RunGen != m.runGen {
		return nil
	}
	m.pendingOutput = append(m.pendingOutput, msg.Data...)
	if len(m.pendingOutput) > lazygitMaxBufferedBytes {
		overflow := len(m.pendingOutput) - lazygitMaxBufferedBytes
		m.pendingOutput = append([]byte(nil), m.pendingOutput[overflow:]...)
	}
	m.lastOutputAt = time.Now()
	if !m.flushScheduled {
		m.flushScheduled = true
		m.flushPendingSince = m.lastOutputAt
		wsID := msg.WorkspaceID
		gen := msg.RunGen
		return common.SafeTick(lazygitFlushQuiet, func(time.Time) tea.Msg {
			return messages.LazygitPTYFlush{WorkspaceID: wsID, RunGen: gen}
		})
	}
	return nil
}

func (m *LazygitModel) handlePTYFlush(msg messages.LazygitPTYFlush) tea.Cmd {
	if msg.RunGen != m.runGen {
		return nil
	}
	now := time.Now()
	quietFor := now.Sub(m.lastOutputAt)
	pendingFor := time.Duration(0)
	if !m.flushPendingSince.IsZero() {
		pendingFor = now.Sub(m.flushPendingSince)
	}
	if quietFor < lazygitFlushQuiet && pendingFor < lazygitFlushMax {
		delay := lazygitFlushQuiet - quietFor
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		m.flushScheduled = true
		wsID := msg.WorkspaceID
		gen := msg.RunGen
		return common.SafeTick(delay, func(time.Time) tea.Msg {
			return messages.LazygitPTYFlush{WorkspaceID: wsID, RunGen: gen}
		})
	}
	m.flushScheduled = false
	m.flushPendingSince = time.Time{}
	if len(m.pendingOutput) > 0 {
		chunkSize := len(m.pendingOutput)
		if chunkSize > lazygitFlushChunkSize {
			chunkSize = lazygitFlushChunkSize
		}
		chunk := append([]byte(nil), m.pendingOutput[:chunkSize]...)
		copy(m.pendingOutput, m.pendingOutput[chunkSize:])
		m.pendingOutput = m.pendingOutput[:len(m.pendingOutput)-chunkSize]

		m.mu.Lock()
		if m.vt != nil {
			filtered := common.FilterKnownPTYNoiseStream(chunk, &m.ptyNoiseTrailing)
			if len(filtered) > 0 {
				m.vt.Write(filtered)
			}
			m.cachedSnap = nil
			m.cachedVersion = 0
		}
		m.mu.Unlock()

		if len(m.pendingOutput) > 0 {
			m.flushScheduled = true
			m.flushPendingSince = time.Now()
			wsID := msg.WorkspaceID
			gen := msg.RunGen
			return common.SafeTick(lazygitFlushQuiet, func(time.Time) tea.Msg {
				return messages.LazygitPTYFlush{WorkspaceID: wsID, RunGen: gen}
			})
		}
	}
	return nil
}

func (m *LazygitModel) handlePTYStopped(msg messages.LazygitPTYStopped) {
	if msg.RunGen != m.runGen {
		logging.Info("lazygit: PTY stopped (stale gen=%d current=%d) workspace=%s err=%v", msg.RunGen, m.runGen, msg.WorkspaceID, msg.Err)
		return
	}
	m.stopPTYReader()
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	logging.Warn("lazygit: PTY stopped workspace=%s err=%v", msg.WorkspaceID, msg.Err)
}

// TerminalLayer returns a VTermLayer for compositor rendering.
func (m *LazygitModel) TerminalLayer() *compositor.VTermLayer {
	return m.TerminalLayerWithCursorOwner(true)
}

// TerminalLayerWithCursorOwner returns a VTermLayer while enforcing whether
// this pane currently owns cursor rendering.
func (m *LazygitModel) TerminalLayerWithCursorOwner(cursorOwner bool) *compositor.VTermLayer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vt == nil {
		return nil
	}
	version := m.vt.Version()
	showCursor := m.focused && cursorOwner
	if m.cachedSnap != nil && m.cachedVersion == version && m.cachedShowCursor == showCursor {
		return compositor.NewVTermLayer(m.cachedSnap)
	}
	snap := compositor.NewVTermSnapshot(m.vt, showCursor)
	if snap == nil {
		return nil
	}
	m.cachedSnap = snap
	m.cachedVersion = version
	m.cachedShowCursor = showCursor
	return compositor.NewVTermLayer(snap)
}

// View renders the lazygit pane.
func (m *LazygitModel) View() string {
	var b strings.Builder

	m.mu.Lock()
	vt := m.vt
	running := m.running
	if vt != nil {
		vt.ShowCursor = m.focused
	}
	m.mu.Unlock()

	if vt == nil {
		if m.workspace == nil {
			b.WriteString(m.styles.Muted.Render("No workspace selected"))
		} else if !running {
			b.WriteString(m.styles.Muted.Render("lazygit not running"))
		} else {
			b.WriteString(m.styles.Muted.Render("Starting lazygit..."))
		}
		return b.String()
	}

	m.mu.Lock()
	content := vt.Render()
	isScrolled := vt.IsScrolled()
	var scrollInfo string
	if isScrolled {
		offset, total := vt.GetScrollInfo()
		scrollInfo = fmt.Sprintf("%d/%d lines up", offset, total)
	}
	m.mu.Unlock()

	b.WriteString(content)
	if isScrolled {
		b.WriteString("\n")
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground()).
			Background(common.ColorInfo())
		b.WriteString(scrollStyle.Render(" SCROLL: " + scrollInfo + " "))
	}

	result := b.String()
	if m.height > 0 {
		lines := strings.Split(result, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			result = strings.Join(lines, "\n")
		}
	}
	return result
}

// Close shuts down the lazygit PTY.
func (m *LazygitModel) Close() {
	m.stopLazygit()
}
