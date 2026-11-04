package agentstatus

import "github.com/andyrewlee/amux/internal/fsm"

// The prompt detector recognizes OSC 133 section A — "new prompt begins" —
// emitted by shells with shell integration. The Program Status Protocol
// makes it a lifetime boundary: working and blocked records MUST drop when
// a new shell prompt begins, so the tap watches for it alongside 7501
// payloads. It is deliberately its own tiny machine rather than a mode of
// the 7501 scanner: different sequence, different emission semantics, and
// the 7501 table stays the single source of truth for payloads.
//
// The sequence is ESC ] 1 3 3 ; A followed by BEL or ST (ESC \). Only
// section A drops records; sections C (command start) and D (command end)
// do not, so a stray letter is a stay-and-drop rejection.

type promptState int

const (
	pmGround promptState = iota
	pmEsc
	pmOsc
	pm1
	pm3a
	pm3b
	pmSemi
	pmLetter
	pmLetterEsc
)

func (s promptState) String() string {
	switch s {
	case pmEsc:
		return "esc"
	case pmOsc:
		return "osc"
	case pm1:
		return "one"
	case pm3a:
		return "three-a"
	case pm3b:
		return "three-b"
	case pmSemi:
		return "semi"
	case pmLetter:
		return "letter"
	case pmLetterEsc:
		return "letter-esc"
	default:
		return "ground"
	}
}

type promptEvent int

const (
	pmEscByte promptEvent = iota
	pmRBracket
	pmOne
	pmThree
	pmSemicolon
	pmLetterA
	pmLetterOther
	pmTerminator // BEL
	pmBackslash
	pmOther
)

// promptMachine is the checked transition table. Unlike the 7501 scanner
// (where undeclared pairs stay and accumulate payload), every non-ground
// state here must DROP to ground on any byte that is not its continuation:
// staying anchored would let junk prefixes like "]3133;A" or "]1133;A"
// false-positive as prompt marks. Only pmGround stays on non-ESC bytes.
var promptMachine = fsm.MustCompile(fsm.Machine[promptState, promptEvent]{
	Name:    "agentstatus-prompt-detector",
	Initial: pmGround,
	Steps: []fsm.Step[promptState, promptEvent]{
		{From: pmGround, Event: pmEscByte, To: pmEsc},

		{From: pmEsc, Event: pmRBracket, To: pmOsc},
		{From: pmEsc, Event: pmEscByte, To: pmEsc},
		{From: pmEsc, Event: pmOther, To: pmGround},
		{From: pmEsc, Event: pmOne, To: pmGround},
		{From: pmEsc, Event: pmThree, To: pmGround},
		{From: pmEsc, Event: pmSemicolon, To: pmGround},
		{From: pmEsc, Event: pmLetterA, To: pmGround},
		{From: pmEsc, Event: pmLetterOther, To: pmGround},
		{From: pmEsc, Event: pmTerminator, To: pmGround},
		{From: pmEsc, Event: pmBackslash, To: pmGround},

		{From: pmOsc, Event: pmOne, To: pm1},
		{From: pmOsc, Event: pmEscByte, To: pmEsc},
		{From: pmOsc, Event: pmOther, To: pmGround},
		{From: pmOsc, Event: pmRBracket, To: pmGround},
		{From: pmOsc, Event: pmThree, To: pmGround},
		{From: pmOsc, Event: pmSemicolon, To: pmGround},
		{From: pmOsc, Event: pmLetterA, To: pmGround},
		{From: pmOsc, Event: pmLetterOther, To: pmGround},
		{From: pmOsc, Event: pmTerminator, To: pmGround},
		{From: pmOsc, Event: pmBackslash, To: pmGround},

		{From: pm1, Event: pmThree, To: pm3a},
		{From: pm1, Event: pmEscByte, To: pmEsc},
		{From: pm1, Event: pmOther, To: pmGround},
		{From: pm1, Event: pmRBracket, To: pmGround},
		{From: pm1, Event: pmOne, To: pmGround},
		{From: pm1, Event: pmSemicolon, To: pmGround},
		{From: pm1, Event: pmLetterA, To: pmGround},
		{From: pm1, Event: pmLetterOther, To: pmGround},
		{From: pm1, Event: pmTerminator, To: pmGround},
		{From: pm1, Event: pmBackslash, To: pmGround},

		{From: pm3a, Event: pmThree, To: pm3b},
		{From: pm3a, Event: pmEscByte, To: pmEsc},
		{From: pm3a, Event: pmOther, To: pmGround},
		{From: pm3a, Event: pmRBracket, To: pmGround},
		{From: pm3a, Event: pmOne, To: pmGround},
		{From: pm3a, Event: pmSemicolon, To: pmGround},
		{From: pm3a, Event: pmLetterA, To: pmGround},
		{From: pm3a, Event: pmLetterOther, To: pmGround},
		{From: pm3a, Event: pmTerminator, To: pmGround},
		{From: pm3a, Event: pmBackslash, To: pmGround},

		{From: pm3b, Event: pmSemicolon, To: pmSemi},
		{From: pm3b, Event: pmEscByte, To: pmEsc},
		{From: pm3b, Event: pmOther, To: pmGround},
		{From: pm3b, Event: pmRBracket, To: pmGround},
		{From: pm3b, Event: pmOne, To: pmGround},
		{From: pm3b, Event: pmThree, To: pmGround},
		{From: pm3b, Event: pmLetterA, To: pmGround},
		{From: pm3b, Event: pmLetterOther, To: pmGround},
		{From: pm3b, Event: pmTerminator, To: pmGround},
		{From: pm3b, Event: pmBackslash, To: pmGround},

		// Only section A is a prompt boundary; any other letter drops out
		// (the sequence was a different OSC 133 section). The pmSemicolon
		// row is load-bearing: an undeclared pair is a STAY, so without it
		// a doubled semicolon ("\x1b]133;;A BEL") would false-positive as
		// a prompt mark and clear transient records.
		{From: pmSemi, Event: pmLetterA, To: pmLetter},
		{From: pmSemi, Event: pmEscByte, To: pmEsc},
		{From: pmSemi, Event: pmSemicolon, To: pmGround},
		{From: pmSemi, Event: pmOther, To: pmGround},
		{From: pmSemi, Event: pmRBracket, To: pmGround},
		{From: pmSemi, Event: pmOne, To: pmGround},
		{From: pmSemi, Event: pmThree, To: pmGround},
		{From: pmSemi, Event: pmLetterOther, To: pmGround},
		{From: pmSemi, Event: pmTerminator, To: pmGround},
		{From: pmSemi, Event: pmBackslash, To: pmGround},

		{From: pmLetter, Event: pmTerminator, To: pmGround},
		{From: pmLetter, Event: pmEscByte, To: pmLetterEsc},
		{From: pmLetter, Event: pmOther, To: pmGround},
		{From: pmLetter, Event: pmRBracket, To: pmGround},
		{From: pmLetter, Event: pmOne, To: pmGround},
		{From: pmLetter, Event: pmThree, To: pmGround},
		{From: pmLetter, Event: pmSemicolon, To: pmGround},
		{From: pmLetter, Event: pmLetterA, To: pmGround},
		{From: pmLetter, Event: pmLetterOther, To: pmGround},
		{From: pmLetter, Event: pmBackslash, To: pmGround},

		{From: pmLetterEsc, Event: pmBackslash, To: pmGround},
		{From: pmLetterEsc, Event: pmEscByte, To: pmEsc},
		{From: pmLetterEsc, Event: pmRBracket, To: pmOsc},
		{From: pmLetterEsc, Event: pmOther, To: pmGround},
		{From: pmLetterEsc, Event: pmOne, To: pmGround},
		{From: pmLetterEsc, Event: pmThree, To: pmGround},
		{From: pmLetterEsc, Event: pmSemicolon, To: pmGround},
		{From: pmLetterEsc, Event: pmLetterA, To: pmGround},
		{From: pmLetterEsc, Event: pmLetterOther, To: pmGround},
		{From: pmLetterEsc, Event: pmTerminator, To: pmGround},
	},
})

// promptEmitting reports the two edges that complete a prompt mark: BEL
// after the A, or ST (ESC backslash) from the one-byte look-ahead.
func promptEmitting(from promptState, ev promptEvent) bool {
	return (from == pmLetter && ev == pmTerminator) ||
		(from == pmLetterEsc && ev == pmBackslash)
}

func promptEventOf(b byte) promptEvent {
	switch b {
	case 0x1b:
		return pmEscByte
	case ']':
		return pmRBracket
	case '1':
		return pmOne
	case '3':
		return pmThree
	case ';':
		return pmSemicolon
	case 'A':
		return pmLetterA
	case 0x07:
		return pmTerminator
	case '\\':
		return pmBackslash
	default:
		return pmOther
	}
}

// PromptDetector incrementally recognizes OSC 133 prompt marks in a raw
// byte stream, across arbitrary chunk boundaries.
type PromptDetector struct {
	state promptState
}

// Feed consumes bytes and reports whether any completed OSC 133 A prompt
// mark ended within them. A single call collapsing several prompts still
// reports true once — the caller's action (drop transient records) is
// idempotent.
func (d *PromptDetector) Feed(p []byte) bool {
	seen := false
	for _, b := range p {
		ev := promptEventOf(b)
		next, ok := promptMachine.Next(d.state, ev)
		if ok {
			if promptEmitting(d.state, ev) {
				seen = true
			}
			d.state = next
			continue
		}
		// Stay-and-drop: the byte does not advance this machine (payload
		// bytes of other sequences, ordinary text). No state change.
	}
	return seen
}
