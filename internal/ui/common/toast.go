package common

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ToastType identifies the type of toast notification
type ToastType int

const (
	ToastInfo ToastType = iota
	ToastSuccess
	ToastError
	ToastWarning
)

// Toast represents a notification message
type Toast struct {
	Message  string
	Type     ToastType
	Duration time.Duration
}

// ToastModel manages toast notifications
type ToastModel struct {
	current   *Toast
	showUntil time.Time
	styles    Styles
}

// NewToastModel creates a new toast model
func NewToastModel() *ToastModel {
	return &ToastModel{
		styles: DefaultStyles(),
	}
}

// SetStyles updates the toast styles (for theme changes).
func (m *ToastModel) SetStyles(styles Styles) {
	m.styles = styles
}

// ToastDismissed is sent when a toast should be dismissed
type ToastDismissed struct{}

// Show displays a toast notification
func (m *ToastModel) Show(message string, toastType ToastType, duration time.Duration) tea.Cmd {
	m.current = &Toast{
		Message:  message,
		Type:     toastType,
		Duration: duration,
	}
	m.showUntil = time.Now().Add(duration)

	return tea.Tick(duration, func(t time.Time) tea.Msg {
		return ToastDismissed{}
	})
}

// ShowSuccess shows a success toast
func (m *ToastModel) ShowSuccess(message string) tea.Cmd {
	return m.Show(message, ToastSuccess, 3*time.Second)
}

// ShowError shows an error toast
func (m *ToastModel) ShowError(message string) tea.Cmd {
	return m.Show(message, ToastError, 5*time.Second)
}

// ShowInfo shows an info toast
func (m *ToastModel) ShowInfo(message string) tea.Cmd {
	return m.Show(message, ToastInfo, 3*time.Second)
}

// ShowWarning shows a warning toast
func (m *ToastModel) ShowWarning(message string) tea.Cmd {
	return m.Show(message, ToastWarning, 4*time.Second)
}

// Update handles messages
func (m *ToastModel) Update(msg tea.Msg) (*ToastModel, tea.Cmd) {
	switch msg.(type) {
	case ToastDismissed:
		if time.Now().After(m.showUntil) {
			m.current = nil
		}
	}
	return m, nil
}

// View renders the toast notification
func (m *ToastModel) View() string {
	if m.current == nil {
		return ""
	}

	// Check if expired
	if time.Now().After(m.showUntil) {
		m.current = nil
		return ""
	}

	var style lipgloss.Style
	var icon string

	switch m.current.Type {
	case ToastSuccess:
		style = m.styles.ToastSuccess
		icon = Icons.Clean + " "
	case ToastError:
		style = m.styles.ToastError
		icon = Icons.Dirty + " "
	case ToastWarning:
		style = m.styles.ToastWarning
		icon = "! "
	case ToastInfo:
		style = m.styles.ToastInfo
		icon = "i "
	}

	return style.Render(icon + m.current.Message)
}

// Visible returns whether the toast is currently visible
func (m *ToastModel) Visible() bool {
	return m.current != nil && time.Now().Before(m.showUntil)
}

// Dismiss immediately hides the toast
func (m *ToastModel) Dismiss() {
	m.current = nil
}
