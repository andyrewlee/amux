package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func TestUnroutedMessageToCenter(t *testing.T) {
	t.Run("center-internal types are expected default traffic", func(t *testing.T) {
		if unroutedMessageToCenter(center.PTYFlush{}) {
			t.Fatal("center.PTYFlush must not be flagged as unrouted")
		}
	})

	t.Run("messages-package types are unrouted", func(t *testing.T) {
		if !unroutedMessageToCenter(messages.Toast{}) {
			t.Fatal("messages.Toast reaching default is a missing dispatch case")
		}
	})

	t.Run("tea types are unrouted", func(t *testing.T) {
		if !unroutedMessageToCenter(tea.KeyPressMsg{}) {
			t.Fatal("tea.KeyPressMsg reaching default is a missing dispatch case")
		}
	})

	t.Run("nil msg is not flagged", func(t *testing.T) {
		if unroutedMessageToCenter(nil) {
			t.Fatal("nil msg should not be flagged")
		}
	})
}
