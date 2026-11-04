package app

import (
	"errors"

	"github.com/andyrewlee/amux/internal/fsm"
)

// prefixModeState models prefix (leader-key) mode over prefixActive /
// prefixSequence (app_prefix.go, app_input_keys.go). The narrowing depth
// (len(prefixSequence)), the typed tokens, and the prefixToken timeout
// generation are extended state; this table is the control shape. The
// timeout token already guards stale timeouts rigorously (a timeout fires
// only when its generation is current and mode is still active), so the
// machine's job is the exit discipline: every way out of prefix mode is an
// explicit complete / mismatch / timeout / Esc / literal-send edge — there
// is no silent deactivation.
type prefixModeState uint8

const (
	// prefixModeInactive: no prefix sequence in progress.
	prefixModeInactive prefixModeState = iota
	// prefixModeRoot: prefix active, no tokens typed (the root command list).
	prefixModeRoot
	// prefixModeNarrowing: prefix active, a partial sequence typed.
	prefixModeNarrowing
)

func (s prefixModeState) String() string {
	switch s {
	case prefixModeRoot:
		return "root"
	case prefixModeNarrowing:
		return "narrowing"
	default:
		return "inactive"
	}
}

// prefixModeEvent is the prefix machine's alphabet.
type prefixModeEvent uint8

const (
	// prefixEvtEnter: the prefix key entered mode (enterPrefix /
	// openCommandsPalette from inactive).
	prefixEvtEnter prefixModeEvent = iota
	// prefixEvtExtend: a key extended the sequence (root's first token, or
	// a narrowing step at depth ≥ 1).
	prefixEvtExtend
	// prefixEvtNarrowReset: the prefix key pressed mid-sequence — restart
	// from the root command list.
	prefixEvtNarrowReset
	// prefixEvtBackspaceNarrow: Backspace at depth ≥ 2 — still narrowing.
	prefixEvtBackspaceNarrow
	// prefixEvtBackspaceToRoot: Backspace at depth 1 — the sequence is
	// empty again, back at the root list.
	prefixEvtBackspaceToRoot
	// prefixEvtBackspaceAtRoot: Backspace at the root — a harmless no-op.
	prefixEvtBackspaceAtRoot
	// prefixEvtMatchComplete: the sequence matched exactly (or a 1-9 tab
	// jump) — dispatch and exit.
	prefixEvtMatchComplete
	// prefixEvtMismatch: an unknown key in prefix mode — exit and fall
	// through to normal handling.
	prefixEvtMismatch
	// prefixEvtTimeout: the prefix timeout elapsed (generation-checked).
	prefixEvtTimeout
	// prefixEvtEsc: Esc cancels prefix mode without forwarding.
	prefixEvtEsc
	// prefixEvtLiteralSend: prefix + prefix at the root — send a literal
	// Ctrl+Space to the terminal and exit.
	prefixEvtLiteralSend
)

func (e prefixModeEvent) String() string {
	switch e {
	case prefixEvtEnter:
		return "enter"
	case prefixEvtExtend:
		return "extend"
	case prefixEvtNarrowReset:
		return "narrow-reset"
	case prefixEvtBackspaceNarrow:
		return "backspace-narrow"
	case prefixEvtBackspaceToRoot:
		return "backspace-to-root"
	case prefixEvtBackspaceAtRoot:
		return "backspace-at-root"
	case prefixEvtMatchComplete:
		return "match-complete"
	case prefixEvtMismatch:
		return "mismatch"
	case prefixEvtTimeout:
		return "timeout"
	case prefixEvtEsc:
		return "esc"
	default:
		return "literal-send"
	}
}

// prefixModeMachine is the prefix-mode table.
var prefixModeMachine = fsm.MustCompile(fsm.Machine[prefixModeState, prefixModeEvent]{
	Name:    "prefix-mode",
	Initial: prefixModeInactive,
	Steps: []fsm.Step[prefixModeState, prefixModeEvent]{
		{From: prefixModeInactive, Event: prefixEvtEnter, To: prefixModeRoot},
		{From: prefixModeRoot, Event: prefixEvtExtend, To: prefixModeNarrowing},
		{From: prefixModeRoot, Event: prefixEvtBackspaceAtRoot, To: prefixModeRoot},
		{From: prefixModeRoot, Event: prefixEvtMatchComplete, To: prefixModeInactive},
		{From: prefixModeRoot, Event: prefixEvtMismatch, To: prefixModeInactive},
		{From: prefixModeRoot, Event: prefixEvtTimeout, To: prefixModeInactive},
		{From: prefixModeRoot, Event: prefixEvtEsc, To: prefixModeInactive},
		{From: prefixModeRoot, Event: prefixEvtLiteralSend, To: prefixModeInactive},
		{From: prefixModeNarrowing, Event: prefixEvtExtend, To: prefixModeNarrowing},
		{From: prefixModeNarrowing, Event: prefixEvtNarrowReset, To: prefixModeRoot},
		{From: prefixModeNarrowing, Event: prefixEvtBackspaceNarrow, To: prefixModeNarrowing},
		{From: prefixModeNarrowing, Event: prefixEvtBackspaceToRoot, To: prefixModeRoot},
		{From: prefixModeNarrowing, Event: prefixEvtMatchComplete, To: prefixModeInactive},
		{From: prefixModeNarrowing, Event: prefixEvtMismatch, To: prefixModeInactive},
		{From: prefixModeNarrowing, Event: prefixEvtTimeout, To: prefixModeInactive},
		{From: prefixModeNarrowing, Event: prefixEvtEsc, To: prefixModeInactive},
	},
})

// prefixModeCheck formally verifies the prefix discipline: the only way IN
// is the enter edge from inactive (no event reaches an active state from
// another active state except within narrowing), every active-state event
// either stays active or exits through one of the five explicit exit
// edges, and both active states can always still exit (no deadlock while
// narrowing — timeout is declared from everywhere active).
func prefixModeCheck() error {
	return prefixModeMachine.Check(fsm.Checker[prefixModeState, prefixModeEvent]{
		Alphabet: []prefixModeEvent{
			prefixEvtEnter, prefixEvtExtend, prefixEvtNarrowReset,
			prefixEvtBackspaceNarrow, prefixEvtBackspaceToRoot, prefixEvtBackspaceAtRoot,
			prefixEvtMatchComplete, prefixEvtMismatch, prefixEvtTimeout, prefixEvtEsc,
			prefixEvtLiteralSend,
		},
		AllDeclaredReachable: true,
		Edge: func(from prefixModeState, event prefixModeEvent, to prefixModeState) error {
			if from == prefixModeInactive && event != prefixEvtEnter && to != prefixModeInactive {
				return errors.New("prefix mode is entered only through the enter edge")
			}
			if to == prefixModeInactive && from != prefixModeInactive {
				switch event {
				case prefixEvtMatchComplete, prefixEvtMismatch, prefixEvtTimeout, prefixEvtEsc, prefixEvtLiteralSend:
					return nil
				default:
					return errors.New("prefix mode may only exit via complete/mismatch/timeout/Esc/literal-send")
				}
			}
			return nil
		},
	})
}
