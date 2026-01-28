package app

import (
	"fmt"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) openURL(url string) tea.Cmd {
	if url == "" {
		return a.toast.ShowInfo("No URL")
	}
	return func() tea.Msg {
		if err := openURLNow(url); err != nil {
			return messages.Error{Err: fmt.Errorf("failed to open URL: %v", err), Context: "open url"}
		}
		return nil
	}
}

func openURLNow(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
