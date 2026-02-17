package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type overlayStub struct {
	visible bool
	updated bool
}

func (o *overlayStub) Visible() bool { return o.visible }

func (o *overlayStub) Update(tea.Msg) (*overlayStub, tea.Cmd) {
	o.updated = true
	return o, nil
}

func TestHandleOverlayInput_NilTypedPointer(t *testing.T) {
	var overlay *overlayStub
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.KeyPressMsg{}, &cmds, true)
	if consumed {
		t.Fatal("expected nil overlay input not to be consumed")
	}
	if updated != nil {
		t.Fatal("expected updated overlay to remain nil")
	}
	if len(cmds) != 0 {
		t.Fatal("expected no commands for nil overlay")
	}
}

func TestHandleOverlayInput_VisibleOverlayConsumesKey(t *testing.T) {
	overlay := &overlayStub{visible: true}
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.KeyPressMsg{}, &cmds, true)
	if !consumed {
		t.Fatal("expected key input to be consumed for visible overlay")
	}
	if !updated.updated {
		t.Fatal("expected visible overlay to receive Update")
	}
}
