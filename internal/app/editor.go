package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) openFileInEditor(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	if a.activeWorkspace != nil && !filepath.IsAbs(path) {
		path = filepath.Join(a.activeWorkspace.Root, path)
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return a.toast.ShowInfo("$EDITOR not set")
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return a.toast.ShowInfo("$EDITOR not set")
	}
	name := parts[0]
	args := append(parts[1:], path)
	return func() tea.Msg {
		cmd := exec.Command(name, args...)
		if err := cmd.Start(); err != nil {
			return messages.Error{Err: fmt.Errorf("failed to open editor: %v", err), Context: "open editor"}
		}
		return nil
	}
}
