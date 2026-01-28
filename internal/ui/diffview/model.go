package diffview

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"

	"github.com/andyrewlee/amux/internal/diff"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model renders diffs.
type Model struct {
	Files []diff.File

	focused bool
	width   int
	height  int

	selected int
	scroll   int

	wrap             bool
	ignoreWhitespace bool
	unified          bool
	collapsed        map[string]bool
	commentCounts    map[string]int
	comments         map[string][]string

	styles common.Styles

	showKeymapHints bool
	zone            *zone.Manager
}

// New creates a diff view model.
func New() *Model {
	return &Model{
		styles:    common.DefaultStyles(),
		unified:   true,
		collapsed: make(map[string]bool),
	}
}

// Init initializes the diff view.
func (m *Model) Init() tea.Cmd { return nil }

// SetShowKeymapHints toggles hints.
func (m *Model) SetShowKeymapHints(show bool) { m.showKeymapHints = show }

// SetZone sets the shared zone manager.
func (m *Model) SetZone(z *zone.Manager) { m.zone = z }

// SetSize sets dimensions.
func (m *Model) SetSize(width, height int) { m.width, m.height = width, height }

// Focus sets focus.
func (m *Model) Focus() { m.focused = true }

// Blur removes focus.
func (m *Model) Blur() { m.focused = false }

// Focused returns focus state.
func (m *Model) Focused() bool { return m.focused }

// SetFiles sets diff files.
func (m *Model) SetFiles(files []diff.File) {
	m.Files = files
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	for _, file := range files {
		if len(file.Lines) > 300 {
			m.collapsed[file.Path] = true
		}
	}
}

// SetCommentCounts sets comment counts by file path.
func (m *Model) SetCommentCounts(counts map[string]int) {
	m.commentCounts = counts
}

// SetComments sets inline comments by key (file:line).
func (m *Model) SetComments(comments map[string][]string) {
	m.comments = comments
}

// IgnoreWhitespace returns current whitespace-ignore setting.
func (m *Model) IgnoreWhitespace() bool { return m.ignoreWhitespace }

// Update handles input.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.move(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.move(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("u"))):
			m.unified = !m.unified
		case key.Matches(msg, key.NewBinding(key.WithKeys("i"))):
			m.ignoreWhitespace = !m.ignoreWhitespace
			return m, func() tea.Msg { return messages.ReloadDiff{IgnoreWhitespace: m.ignoreWhitespace} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("w"))):
			m.wrap = !m.wrap
		case key.Matches(msg, key.NewBinding(key.WithKeys("x"))):
			m.toggleCollapse()
		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			file, line, side := m.selectedLocation()
			return m, func() tea.Msg { return messages.ShowDiffCommentDialog{File: file, Line: line, Side: side} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
			file, _, _ := m.selectedLocation()
			return m, func() tea.Msg { return messages.OpenFileInEditor{File: file} }
		}
	case tea.MouseClickMsg:
		return m.handleMouse(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	// Zone-based click handling temporarily disabled for bubbletea v2 migration
	// TODO: Implement hit-region based click handling
	return m, nil
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (*Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.move(-1)
	case tea.MouseWheelDown:
		m.move(1)
	}
	return m, nil
}

func (m *Model) move(delta int) {
	lines := m.flatten()
	if len(lines) == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(lines) {
		m.selected = len(lines) - 1
	}
}

func (m *Model) toggleCollapse() {
	lines := m.flatten()
	if len(lines) == 0 {
		return
	}
	line := lines[m.selected]
	if line.FilePath == "" {
		return
	}
	m.toggleCollapsePath(line.FilePath)
}

func (m *Model) allCollapsed() bool {
	if len(m.Files) == 0 {
		return false
	}
	for _, file := range m.Files {
		if !m.collapsed[file.Path] {
			return false
		}
	}
	return true
}

func (m *Model) toggleCollapsePath(path string) {
	if path == "" {
		return
	}
	m.collapsed[path] = !m.collapsed[path]
}

func (m *Model) selectedLocation() (string, int, string) {
	lines := m.flatten()
	if len(lines) == 0 || m.selected < 0 || m.selected >= len(lines) {
		return "", 0, ""
	}
	line := lines[m.selected]
	if line.NewLine != 0 {
		return line.FilePath, line.NewLine, lineSide(line)
	}
	return line.FilePath, line.OldLine, lineSide(line)
}

func (m *Model) flatten() []diff.Line {
	var out []diff.Line
	for _, file := range m.Files {
		path := file.Path
		headerText := fmt.Sprintf("%s (+%d -%d)", path, file.Added, file.Deleted)
		if m.commentCounts != nil {
			if count := m.commentCounts[path]; count > 0 {
				headerText = fmt.Sprintf("%s [%d]", headerText, count)
			}
		}
		header := diff.Line{Text: headerText, Type: diff.LineHeader, FilePath: path}
		out = append(out, header)
		if m.collapsed[path] {
			continue
		}
		for _, line := range file.Lines {
			line.FilePath = path
			out = append(out, line)
			if m.comments != nil && line.FilePath != "" {
				commentKey := commentKeyForLine(line)
				if commentKey != "" {
					if comments := m.comments[commentKey]; len(comments) > 0 {
						for _, comment := range comments {
							if len(comment) == 0 {
								continue
							}
							out = append(out, diff.Line{
								Text:     comment,
								Type:     diff.LineComment,
								OldLine:  line.OldLine,
								NewLine:  line.NewLine,
								FilePath: line.FilePath,
							})
						}
					}
				}
			}
		}
	}
	return out
}

func commentKeyForLine(line diff.Line) string {
	if line.FilePath == "" {
		return ""
	}
	lineNo := 0
	if line.NewLine != 0 {
		lineNo = line.NewLine
	} else if line.OldLine != 0 {
		lineNo = line.OldLine
	}
	if lineNo == 0 {
		return ""
	}
	return fmt.Sprintf("%s::%s::%d", line.FilePath, lineSide(line), lineNo)
}

func lineSide(line diff.Line) string {
	switch line.Type {
	case diff.LineDel:
		return "old"
	case diff.LineAdd:
		return "new"
	default:
		if line.NewLine != 0 {
			return "new"
		}
		return "old"
	}
}

func lineContent(line diff.Line) string {
	if line.Text == "" {
		return ""
	}
	switch line.Text[0] {
	case '+', '-', ' ':
		if len(line.Text) > 1 {
			return line.Text[1:]
		}
		return ""
	default:
		return line.Text
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
