package common

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// ReportError logs, emits an Error message, and shows a toast.
func ReportError(context string, err error, toastMessage string) tea.Cmd {
	if err == nil {
		return nil
	}
	logging.Error("Error in %s: %v", context, err)
	message := toastMessage
	if message == "" {
		message = err.Error()
	}
	return SafeBatch(
		func() tea.Msg {
			return messages.Error{Err: err, Context: context, Logged: true}
		},
		func() tea.Msg {
			return messages.Toast{Message: message, Level: messages.ToastError}
		},
	)
}
