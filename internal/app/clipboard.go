package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) copyToClipboard(text string) tea.Cmd {
	if text == "" {
		return a.toast.ShowInfo("Nothing to copy")
	}
	return func() tea.Msg {
		if err := clipboard.WriteAll(text); err != nil {
			return messages.Error{Err: fmt.Errorf("clipboard error: %v", err), Context: "clipboard"}
		}
		return nil
	}
}
