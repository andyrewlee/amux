package agentstatus

import (
	"github.com/andyrewlee/amux/internal/fsm"
)

// The tap scanner extracts complete OSC 7501 payloads from a raw pane byte
// stream. It is deliberately not a VT parser: the pipe-pane tap only needs
// one sequence, mis-skipping an unrelated escape costs nothing (the tap
// never alters bytes), and a full parser would drag in grid semantics this
// path has no use for.
//
// The transition table below is the single source of truth for both the
// runtime loop and the formal model check in scanner_model_test.go. Bytes
// reduce to byteClass events; the intro accumulator and the payload-length
// counter are extended state the call site reduces to machine events
// (evIntroMatch/evIntroReject on ';', an overflow move to stSkip).

type scanState int

const (
	stGround scanState = iota
	stEsc
	stIntro
	stPayload
	stPayloadEsc
	stSkip
	stSkipEsc
)

func (s scanState) String() string {
	switch s {
	case stEsc:
		return "esc"
	case stIntro:
		return "intro"
	case stPayload:
		return "payload"
	case stPayloadEsc:
		return "payload-esc"
	case stSkip:
		return "skip"
	case stSkipEsc:
		return "skip-esc"
	default:
		return "ground"
	}
}

type byteClass int

const (
	clsESC byteClass = iota
	clsRBracket
	clsBEL
	clsBackslash
	clsSemicolon
	clsCAN
	clsOther
)

// evIntroMatch and evIntroReject are the call-site reduction of a ';' in
// the intro state: the accumulated intro bytes either spell "7501" or they
// do not. They share the concrete byte with clsSemicolon; the classes and
// the reduction events never collide because the reduction only applies in
// stIntro.
const (
	evIntroMatch byteClass = 100 + iota
	evIntroReject
)

// introMax is the longest OSC number the scanner will accumulate before
// giving up on the sequence. "7501" is four bytes; anything past eight is
// never a match and stops wasting buffer.
const introMax = 8

// scannerMachine is the checked transition table. Rows exist only for moves
// that change state: an undeclared (state, class) pair is a stay-and-drop
// rejection, the package-wide fail-closed convention, so accumulating a
// payload byte or an intro digit needs no row.
var scannerMachine = fsm.MustCompile(fsm.Machine[scanState, byteClass]{
	Name:    "agentstatus-scanner",
	Initial: stGround,
	Steps: []fsm.Step[scanState, byteClass]{
		{From: stGround, Event: clsESC, To: stEsc},

		// After ESC, only '['-style dispatches continue a sequence; for
		// this tap only ']' does. Every other byte is ground data — a
		// two-byte escape such as ESC ( B must not leave the scanner
		// waiting for a ']', or literal "]7501;..." text that follows it
		// would falsely introduce a sequence.
		{From: stEsc, Event: clsRBracket, To: stIntro},
		{From: stEsc, Event: clsESC, To: stEsc},
		{From: stEsc, Event: clsOther, To: stGround},
		{From: stEsc, Event: clsBEL, To: stGround},
		{From: stEsc, Event: clsBackslash, To: stGround},
		{From: stEsc, Event: clsSemicolon, To: stGround},
		{From: stEsc, Event: clsCAN, To: stGround},

		{From: stIntro, Event: evIntroMatch, To: stPayload},
		{From: stIntro, Event: evIntroReject, To: stSkip},
		{From: stIntro, Event: clsESC, To: stEsc},
		{From: stIntro, Event: clsCAN, To: stGround},

		{From: stPayload, Event: clsBEL, To: stGround},
		{From: stPayload, Event: clsESC, To: stPayloadEsc},
		{From: stPayload, Event: clsCAN, To: stGround},

		// After ESC inside a payload, only '\' (ST) completes it. A second
		// ESC aborts and re-anchors; ']' right after the aborting ESC
		// opens a fresh sequence (VT semantics: ESC terminates the OSC
		// and begins the next escape), so an unterminated junk sequence
		// cannot swallow the report that follows it.
		{From: stPayloadEsc, Event: clsBackslash, To: stGround},
		{From: stPayloadEsc, Event: clsESC, To: stEsc},
		{From: stPayloadEsc, Event: clsRBracket, To: stIntro},
		{From: stPayloadEsc, Event: clsOther, To: stGround},
		{From: stPayloadEsc, Event: clsBEL, To: stGround},
		{From: stPayloadEsc, Event: clsSemicolon, To: stGround},
		{From: stPayloadEsc, Event: clsCAN, To: stGround},

		{From: stSkip, Event: clsBEL, To: stGround},
		{From: stSkip, Event: clsESC, To: stSkipEsc},
		{From: stSkip, Event: clsCAN, To: stGround},

		{From: stSkipEsc, Event: clsBackslash, To: stGround},
		{From: stSkipEsc, Event: clsESC, To: stEsc},
		{From: stSkipEsc, Event: clsRBracket, To: stIntro},
		{From: stSkipEsc, Event: clsOther, To: stGround},
		{From: stSkipEsc, Event: clsBEL, To: stGround},
		{From: stSkipEsc, Event: clsSemicolon, To: stGround},
		{From: stSkipEsc, Event: clsCAN, To: stGround},
	},
})

// emitting reports the two edges that complete a 7501 sequence and release
// its payload: BEL in the payload state, or ST (ESC backslash) from the
// one-byte look-ahead state. The model check pins this predicate to exactly
// those edges so an emission can never sneak onto a skip or abort path.
func emitting(from scanState, ev byteClass) bool {
	return (from == stPayload && ev == clsBEL) || (from == stPayloadEsc && ev == clsBackslash)
}

// classOf reduces one byte to its event class. CAN and SUB both abort
// sequences in VT practice and share clsCAN.
func classOf(b byte) byteClass {
	switch b {
	case 0x1b:
		return clsESC
	case ']':
		return clsRBracket
	case 0x07:
		return clsBEL
	case '\\':
		return clsBackslash
	case ';':
		return clsSemicolon
	case 0x18, 0x1a:
		return clsCAN
	default:
		return clsOther
	}
}

// Scanner incrementally extracts OSC 7501 payloads. Feed may be called with
// arbitrary chunk boundaries — a sequence split across reads is assembled,
// because PTY output arrives in unpredictable slices.
type Scanner struct {
	state scanState
	intro []byte
	buf   []byte
}

// Feed consumes bytes and returns every payload completed by this call. The
// returned slice is valid until the next Feed; callers copy what they keep.
func (s *Scanner) Feed(p []byte) []string {
	var out []string
	for i := 0; i < len(p); i++ {
		b := p[i]
		cls := classOf(b)
		// The call-site reduction: ';' in the intro state becomes a match
		// or reject event once the accumulated intro is known. Everything
		// else steps the shared table directly.
		if s.state == stIntro && cls == clsSemicolon {
			if string(s.intro) == "7501" {
				cls = evIntroMatch
			} else {
				cls = evIntroReject
			}
		}
		next, ok := scannerMachine.Next(s.state, cls)
		if !ok {
			s.stay(b)
			continue
		}
		if emitting(s.state, cls) {
			out = append(out, string(s.buf))
		}
		// A completed or aborted sequence releases its buffer; the only
		// transition that carries it forward is the ESC look-ahead from
		// payload into payload-esc, where '\' still completes it.
		if (s.state == stPayload && next != stPayloadEsc) || s.state == stPayloadEsc {
			s.buf = s.buf[:0]
		}
		if next == stIntro && s.state != stIntro {
			s.intro = s.intro[:0]
		}
		s.state = next
	}
	return out
}

// stay handles the fail-closed case of an undeclared (state, class) pair:
// the byte either accumulates into the pending intro or payload, or — once
// the accumulated length breaks its cap — the whole sequence is dropped
// into stSkip, so a hostile stream cannot grow memory without bound. Any
// other state ignores the byte and keeps waiting for a terminator.
func (s *Scanner) stay(b byte) {
	switch s.state {
	case stPayload:
		s.buf = append(s.buf, b)
		if len(s.buf) > MaxPayloadBytes {
			s.buf = s.buf[:0]
			s.state = stSkip
		}
	case stIntro:
		s.intro = append(s.intro, b)
		if len(s.intro) > introMax {
			s.intro = s.intro[:0]
			s.state = stSkip
		}
	}
}
