package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// reportError logs, emits an Error message, and shows a toast.
func (a *App) reportError(context string, err error, toastMessage string) tea.Cmd {
	if err == nil {
		return nil
	}
	logging.Error("Error in %s: %v", context, err)
	message := toastMessage
	if message == "" {
		message = err.Error()
	}
	return a.safeBatch(
		func() tea.Msg {
			return messages.Error{Err: err, Context: context, Logged: true}
		},
		func() tea.Msg {
			return messages.Toast{Message: message, Level: messages.ToastError}
		},
	)
}

func (a *App) handleErrorMessage(msg messages.Error) tea.Cmd {
	if msg.Err == nil {
		return nil
	}
	a.err = msg.Err
	if !msg.Logged {
		logging.Error("Error in %s: %v", msg.Context, msg.Err)
	}
	return nil
}
