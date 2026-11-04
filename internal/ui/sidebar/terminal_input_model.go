// terminal_input_model.go holds the sidebar terminal input-binding protocol
// machine — the gen/failed latch on sidebarInputState (terminal_input.go),
// kept as fields with the protocol state derived from them, mirroring
// ptyio's ReattachGuard model.

package sidebar

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// inputBindingState is the input binding's control state, derived from
// sidebarInputState's fields (the fields are the extended state; the
// derivation below is the single mapping between them and the protocol
// machine).
type inputBindingState int

const (
	// inputUnbound: no writer installed — admission lazily creates one.
	inputUnbound inputBindingState = iota
	// inputBound: a writer is installed and no failure is latched.
	inputBound
	// inputFailed: the live writer's send failed; admission rejects until
	// the binding is retired (detach/teardown/replacement).
	inputFailed
)

func (s inputBindingState) String() string {
	switch s {
	case inputBound:
		return "bound"
	case inputFailed:
		return "failed"
	default:
		return "unbound"
	}
}

// inputBindingEvent is the binding protocol's alphabet.
type inputBindingEvent int

const (
	evtInputBind     inputBindingEvent = iota // admission installed a writer under the current gen
	evtInputSendFail                          // the live writer's send failed (latches)
	evtInputRetire                            // detach/teardown/replacement retired the binding and bumped gen
)

func (e inputBindingEvent) String() string {
	switch e {
	case evtInputBind:
		return "bind"
	case evtInputSendFail:
		return "send-fail"
	default:
		return "retire"
	}
}

// inputBindingMachine is the binding protocol: admission binds lazily from
// unbound and never over a live writer; a failed send latches on the bound
// binding only — a failure stamped under a retired gen (writer mismatch)
// must not latch state the replacement would inherit; and retire always
// returns to unbound with the gen bumped, clearing the latch.
var inputBindingMachine = fsm.MustCompile(fsm.Machine[inputBindingState, inputBindingEvent]{
	Name:    "sidebar-input-binding",
	Initial: inputUnbound,
	Steps: []fsm.Step[inputBindingState, inputBindingEvent]{
		{From: inputUnbound, Event: evtInputBind, To: inputBound},
		{From: inputBound, Event: evtInputSendFail, To: inputFailed},
		{From: inputBound, Event: evtInputRetire, To: inputUnbound},
		{From: inputFailed, Event: evtInputRetire, To: inputUnbound},
	},
})

// State derives the binding's protocol state from its fields.
func (s *sidebarInputState) state() inputBindingState {
	switch {
	case s.writer == nil:
		return inputUnbound
	case s.failed:
		return inputFailed
	default:
		return inputBound
	}
}

// inputBindingCheck formally verifies the binding protocol: every state
// reachable, a send failure latches only on the bound binding, retire always
// unbinds (and is the only unlatch), and a bind happens only from unbound —
// admission never replaces a live writer.
func inputBindingCheck() error {
	return inputBindingMachine.Check(fsm.Checker[inputBindingState, inputBindingEvent]{
		Alphabet:             []inputBindingEvent{evtInputBind, evtInputSendFail, evtInputRetire},
		AllDeclaredReachable: true,
		Edge: func(from inputBindingState, event inputBindingEvent, to inputBindingState) error {
			switch event {
			case evtInputSendFail:
				if from != inputBound {
					return fmt.Errorf("a send failure latches only on a live binding, not %v", from)
				}
			case evtInputRetire:
				if to != inputUnbound {
					return errors.New("retiring a binding must always unbind")
				}
			case evtInputBind:
				if from != inputUnbound || to != inputBound {
					return errors.New("a bind must install a writer only over an unbound state")
				}
			}
			return nil
		},
	})
}
