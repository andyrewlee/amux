package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleOpenFileInVim handles the OpenFileInVim message from the project
// tree and the diff/Changes `v` jump. This opens the file in vim in the
// center pane.
func (a *App) handleOpenFileInVim(msg messages.OpenFileInVim) tea.Cmd {
	if msg.Workspace == nil || msg.Path == "" {
		return nil
	}
	if err := a.regularViewableFile(msg); err != nil {
		// The path a repo ships is attacker-controlled: a symlink renders
		// its target's bytes into a PTY viewer (display-only disclosure —
		// ~/.aws/credentials on screen) and a FIFO/device blocks the pane.
		// Same threat model as GetUntrackedFileContent's containment check.
		logging.Warn("Refusing to open non-regular file %q: %v", msg.Path, err)
		return a.toast.ShowWarning("Refusing to open file: not a regular file in an amux-owned directory")
	}
	logging.Info("Opening file in editor: %s", msg.Path)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// regularViewableFile requires msg.Path to resolve to a regular file under
// one of the allowed roots: the hosting workspace's checkout (diff `v`,
// Changes `v`, project tree — repo-controlled paths) or the amux state
// home (the transcript picker's ~/.amux/transcripts and other amux-owned
// surfaces). Containment via os.OpenRoot is lexical: Lstat refuses `..`
// escapes and reports links themselves, so symlinks (ModeSymlink), FIFOs,
// and devices all fail IsRegular — only a real contained file opens.
func (a *App) regularViewableFile(msg messages.OpenFileInVim) error {
	roots := []string{msg.Workspace.Root}
	if a.config != nil && a.config.Paths != nil {
		roots = append(roots, a.config.Paths.Home)
	}
	var lastErr error
	for _, root := range roots {
		if root == "" {
			continue
		}
		err := regularFileUnder(root, msg.Path)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no allowed roots")
	}
	return lastErr
}

// regularFileUnder requires path to resolve to a regular file inside root.
func regularFileUnder(root, path string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	info, err := r.Lstat(rel)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file (mode %s)", info.Mode())
	}
	return nil
}
