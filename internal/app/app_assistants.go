package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func (a *App) defaultAssistantName() string {
	return data.DefaultAssistant
}

func (a *App) assistantNames() []string {
	if a != nil && a.config != nil {
		names := a.config.AssistantNames()
		if len(names) > 0 {
			return names
		}
	}
	return []string{a.defaultAssistantName()}
}

func (a *App) isKnownAssistant(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if a == nil || a.config == nil || len(a.config.Assistants) == 0 {
		return true
	}
	return a.config.IsAssistantKnown(name)
}
