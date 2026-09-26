package diff

import (
	"errors"
	"path/filepath"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model is the Bubble Tea model for the native diff viewer
type Model struct {
	// Data
	workspace *data.Workspace
	change    *git.Change
	diff      *git.DiffResult
	mode      git.DiffMode
	loadID    uint64

	// State
	loading bool
	err     error
	scroll  int  // Scroll offset in visual rows (see visual_rows.go)
	hunkIdx int  // Current hunk index for n/p navigation
	wrap    bool // Whether to wrap long lines
	focused bool

	// Layout
	width  int
	height int

	// wrapResult, when set, wraps every async load result so the caller can
	// route it back to the owning tab (load results otherwise carry no
	// destination and land on whichever tab is active).
	wrapResult func(tea.Msg) tea.Msg

	// Styles
	styles common.Styles
	// stylesRev bumps on SetStyles so the view memo key captures style
	// changes without making common.Styles comparable.
	stylesRev uint64

	// viewKey/viewCache memoize View()'s string build — the center compose
	// path calls it every frame while a diff tab is active, and a static
	// diff re-renders identically until an input field moves.
	viewKey   diffViewKey
	viewCache string
	viewValid bool

	// rowsCache memoizes the visual-row layout on (diff, width, wrap,
	// stylesRev) — scrolling never rebuilds it. rowsBuilt counts builds so
	// tests can prove scrolling reuses the cache.
	rowsCache visualRows
	rowsKey   visualRowsKey
	rowsValid bool
	rowsBuilt int
}

// diffViewKey captures every input View() reads: the loaded data pointers,
// scroll/hunk position, wrap/focus flags, dimensions, and styles revision.
type diffViewKey struct {
	diff      *git.DiffResult
	errStr    string
	loading   bool
	scroll    int
	hunkIdx   int
	wrap      bool
	focused   bool
	width     int
	height    int
	stylesRev uint64
}

// diffLoaded is sent when the diff has been loaded
type diffLoaded struct {
	diff   *git.DiffResult
	err    error
	loadID uint64
}

// New creates a new diff viewer model
func New(ws *data.Workspace, change *git.Change, mode git.DiffMode, width, height int) *Model {
	return &Model{
		workspace: ws,
		change:    change,
		mode:      mode,
		loading:   true,
		width:     width,
		height:    height,
		styles:    common.DefaultStyles(),
	}
}

// Init initializes the diff viewer and starts loading the diff
func (m *Model) Init() tea.Cmd {
	return m.loadDiff()
}

func normalizeSourcePath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// MatchesSource reports whether the viewer is already showing the same diff target.
func (m *Model) MatchesSource(changePath string, mode git.DiffMode) bool {
	if m == nil || m.change == nil {
		return false
	}
	return normalizeSourcePath(m.change.Path) == normalizeSourcePath(changePath) && m.mode == mode
}

// ResetSource updates the diff target before a reload while preserving viewer UI state.
func (m *Model) ResetSource(ws *data.Workspace, change *git.Change, mode git.DiffMode) {
	m.workspace = ws
	m.change = change
	m.mode = mode
	m.loading = true
	m.err = nil
	m.diff = nil
	m.scroll = 0
	m.hunkIdx = 0
	m.invalidateRows()
}

// loadDiff returns a command that loads the diff asynchronously
func (m *Model) loadDiff() tea.Cmd {
	ws := m.workspace
	change := m.change
	mode := m.mode
	m.loadID++
	loadID := m.loadID
	// Snapshot the wrapper now — the closure runs off the update goroutine and
	// must not read model fields.
	wrap := m.wrapResult
	if wrap == nil {
		wrap = func(msg tea.Msg) tea.Msg { return msg }
	}

	return func() tea.Msg {
		if ws == nil || change == nil {
			return wrap(diffLoaded{err: nil, diff: &git.DiffResult{Empty: true}, loadID: loadID})
		}

		var diff *git.DiffResult
		var err error

		switch {
		case change.Kind == git.ChangeUntracked:
			diff, err = git.GetUntrackedFileContent(ws.Root, change.Path)
		case mode == git.DiffModeBranch:
			diff, err = git.GetBranchFileDiff(ws.Root, change.Path)
		default:
			diff, err = git.GetFileDiff(ws.Root, change.Path, mode)
		}

		return wrap(diffLoaded{diff: diff, err: err, loadID: loadID})
	}
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case diffLoaded:
		if msg.loadID != m.loadID {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// git diff failures are carried in DiffResult.Error with a nil Go error;
		// surface them via the error view instead of rendering "No changes".
		if msg.diff != nil && msg.diff.Error != "" {
			m.err = errors.New(msg.diff.Error)
			return m, nil
		}
		m.err = nil
		m.diff = msg.diff
		m.invalidateRows()
		if limit := m.maxScroll(); m.scroll > limit {
			m.scroll = limit
		}
		return m, nil

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.scrollUp(3)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.scrollDown(3)
			return m, nil
		}

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		// Scroll controls
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.scrollDown(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.scrollUp(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))):
			m.scrollDown(common.ScrollDeltaForHeight(m.contentHeight(), 2))
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup", "ctrl+u"))):
			m.scrollUp(common.ScrollDeltaForHeight(m.contentHeight(), 2))
		case key.Matches(msg, key.NewBinding(key.WithKeys("g", "home"))):
			m.scroll = 0
		case key.Matches(msg, key.NewBinding(key.WithKeys("G", "end"))):
			m.scrollToBottom()

		// Hunk navigation
		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			m.nextHunk()
		case key.Matches(msg, key.NewBinding(key.WithKeys("p"))):
			m.prevHunk()

		// Toggle wrap — keep the same source line at the top of the
		// viewport, clamping its segment to the new wrap count.
		case key.Matches(msg, key.NewBinding(key.WithKeys("w"))):
			line, seg := m.rows().anchor(m.scroll)
			m.wrap = !m.wrap
			m.scroll = m.clampScroll(m.rows().rowForAnchor(line, seg))

		// Close
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, func() tea.Msg { return messages.CloseTab{} }
		}
	}

	return m, nil
}

// scrollUp scrolls up by n lines
func (m *Model) scrollUp(n int) {
	m.scroll -= n
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// scrollDown scrolls down by n lines
func (m *Model) scrollDown(n int) {
	m.scroll += n
	maxScroll := m.maxScroll()
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

// scrollToBottom scrolls to the bottom of the diff
func (m *Model) scrollToBottom() {
	m.scroll = m.maxScroll()
}

// maxScroll returns the maximum scroll offset in visual rows. A zero content
// capacity has no scrollable behavior even when rows exist.
func (m *Model) maxScroll() int {
	if m.diff == nil {
		return 0
	}
	capacity := m.contentHeight()
	if capacity < 1 {
		return 0
	}
	if total := len(m.rows().rows); total > capacity {
		return total - capacity
	}
	return 0
}

// clampScroll bounds an offset to the scrollable visual-row range.
func (m *Model) clampScroll(scroll int) int {
	if limit := m.maxScroll(); scroll > limit {
		return limit
	}
	if scroll < 0 {
		return 0
	}
	return scroll
}

// contentHeight returns the display rows available for diff content:
// heights ≥3 reserve header/stats/footer; smaller heights are all chrome.
func (m *Model) contentHeight() int {
	h := m.height - 3
	if h < 0 {
		h = 0
	}
	return h
}

// nextHunk moves to the next hunk's first visual row, wrapping around. The
// target scroll clamps into the scrollable range while hunkIdx tracks the
// selected hunk explicitly, so a last hunk whose top lands past maxScroll
// still advances the cycle instead of getting stuck.
func (m *Model) nextHunk() {
	if m.diff == nil || len(m.diff.Hunks) == 0 {
		return
	}
	rows := m.rows()

	for i, hunk := range m.diff.Hunks {
		if rows.topFor(hunk.StartLine) > m.scroll {
			m.hunkIdx = i
			m.scroll = m.clampScroll(rows.topFor(hunk.StartLine))
			return
		}
	}

	m.hunkIdx = 0
	m.scroll = m.clampScroll(rows.topFor(m.diff.Hunks[0].StartLine))
}

// prevHunk moves to the previous hunk's first visual row, wrapping around.
func (m *Model) prevHunk() {
	if m.diff == nil || len(m.diff.Hunks) == 0 {
		return
	}
	rows := m.rows()

	for i := len(m.diff.Hunks) - 1; i >= 0; i-- {
		if rows.topFor(m.diff.Hunks[i].StartLine) < m.scroll {
			m.hunkIdx = i
			m.scroll = m.clampScroll(rows.topFor(m.diff.Hunks[i].StartLine))
			return
		}
	}

	m.hunkIdx = len(m.diff.Hunks) - 1
	m.scroll = m.clampScroll(rows.topFor(m.diff.Hunks[m.hunkIdx].StartLine))
}

// SetResultWrapper installs a function applied to every async load result
// before it is emitted, so the owner can tag results with routing identity.
func (m *Model) SetResultWrapper(fn func(tea.Msg) tea.Msg) {
	if m == nil {
		return
	}
	m.wrapResult = fn
}

// SetFocused sets the focused state
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
}

// Focus sets the component as focused
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the component is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetSize sets the component dimensions. A width change re-wraps the visual
// rows, so the top of the viewport re-anchors to the same source line with
// its segment clamped to the new segment count; a height-only change keeps
// the current visual offset and just clamps it to the scrollable range.
func (m *Model) SetSize(width, height int) {
	if width == m.width && height == m.height {
		return
	}
	line, seg := m.rows().anchor(m.scroll)
	widthChanged := width != m.width
	m.width = width
	m.height = height
	if widthChanged {
		m.scroll = m.clampScroll(m.rows().rowForAnchor(line, seg))
	} else if limit := m.maxScroll(); m.scroll > limit {
		m.scroll = limit
	}
}

// SetStyles updates the component's styles
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
	m.stylesRev++
}

// GetPath returns the file path being viewed
func (m *Model) GetPath() string {
	if m.change != nil {
		return m.change.Path
	}
	return ""
}

// View is defined in view.go
