package diff

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// openSource jumps from the diff to the file itself — the sibling of the
// scroll/nav keys: it hands the workspace-rooted absolute path to the
// viewer tab the project tree opens. Deleted files have no worktree target,
// so the key reports rather than opening the viewer on an empty buffer.
func (m *Model) openSource() tea.Cmd {
	ws := m.workspace
	change := m.change
	if ws == nil || change == nil || change.Path == "" {
		return nil
	}
	if change.Kind == git.ChangeDeleted {
		return func() tea.Msg {
			return messages.Toast{Message: "File is deleted — nothing to open", Level: messages.ToastInfo}
		}
	}
	path := filepath.Join(ws.Root, change.Path)
	return func() tea.Msg {
		return messages.OpenFileInVim{Path: path, Workspace: ws}
	}
}
