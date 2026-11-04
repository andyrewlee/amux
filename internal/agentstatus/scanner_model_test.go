package agentstatus

import (
	"errors"
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// TestScannerMachineFormallyChecked verifies the tap table's structural
// properties: every state is reachable, nothing deadlocks, an emission
// happens only on the two terminator edges, the payload state is entered
// only through a matched 7501 intro, and no abort or skip path can land in
// a collecting state (payload or intro) — a mis-sequenced stream can never
// smuggle bytes into a later report.
func TestScannerMachineFormallyChecked(t *testing.T) {
	collecting := func(s scanState) bool { return s == stPayload || s == stIntro }
	err := scannerMachine.Check(fsm.Checker[scanState, byteClass]{
		Alphabet: []byteClass{
			clsESC, clsRBracket, clsBEL, clsBackslash, clsSemicolon,
			clsCAN, clsOther, evIntroMatch, evIntroReject,
		},
		AllDeclaredReachable: true,
		Edge: func(from scanState, ev byteClass, to scanState) error {
			if emitting(from, ev) && to != stGround {
				return fmt.Errorf("an emission edge must land in ground (got %v --%v--> %v)", from, ev, to)
			}
			if !emitting(from, ev) && from == stPayload && ev == clsBEL {
				return errors.New("BEL in the payload state must be an emission edge")
			}
			if !emitting(from, ev) && from == stPayloadEsc && ev == clsBackslash {
				return errors.New("ST in the payload-esc state must be an emission edge")
			}
			if to == stPayload && !(from == stIntro && ev == evIntroMatch) {
				return fmt.Errorf("payload state entered from %v --%v-->, want only intro match", from, ev)
			}
			if to == stIntro && ev != clsRBracket {
				return fmt.Errorf("intro state entered from %v --%v-->, want only an OSC open bracket", from, ev)
			}
			if from == stSkip && collecting(to) {
				return fmt.Errorf("a skipped sequence must not resume collecting (got %v)", to)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("scanner machine check failed: %v", err)
	}
	// The two emission edges must exist in the table at all — Check only
	// visits declared edges, so their absence would otherwise pass silently.
	if _, ok := scannerMachine.Next(stPayload, clsBEL); !ok {
		t.Fatal("table is missing the BEL terminator edge from payload")
	}
	if _, ok := scannerMachine.Next(stPayloadEsc, clsBackslash); !ok {
		t.Fatal("table is missing the ST terminator edge from payload-esc")
	}
}

// TestScannerMatchesModel is the differential harness: the runtime Feed
// loop must agree with the table on every event application. Sequences are
// enumerated over one representative byte per class; where the enumerated
// byte cannot produce the intro bytes the reduction needs, the harness
// injects them directly so both reduction branches (match and reject) are
// exercised in every reachable context. Any wiring divergence between the
// switch and the table is caught by construction, not by hand-picked
// examples.
func TestScannerMatchesModel(t *testing.T) {
	letters := []byte{0x1b, ']', ';', 'x', 0x07, '\\', 0x18}
	var enumerate func(prefix []byte, depth int, fn func(seq []byte))
	enumerate = func(prefix []byte, depth int, fn func(seq []byte)) {
		if depth == 0 {
			fn(prefix)
			return
		}
		for _, b := range letters {
			enumerate(append(prefix, b), depth-1, fn)
		}
	}
	enumerate(nil, 6, func(seq []byte) {
		for _, introMatches := range []bool{false, true} {
			s := &Scanner{}
			state := stGround
			for _, b := range seq {
				cls := classOf(b)
				if state == stIntro && cls == clsSemicolon {
					if introMatches {
						s.intro = []byte("7501")
						cls = evIntroMatch
					} else {
						s.intro = []byte("0")
						cls = evIntroReject
					}
				}
				want, ok := scannerMachine.Next(state, cls)
				gotBefore := s.state
				s.Feed([]byte{b})
				if ok && s.state != want {
					t.Fatalf("seq %q (match=%v): byte %q moved runtime to %v, table says %v (from %v)",
						seq, introMatches, b, s.state, want, gotBefore)
				}
				if !ok && s.state != gotBefore {
					t.Fatalf("seq %q (match=%v): byte %q rejected by table but runtime moved %v -> %v",
						seq, introMatches, b, gotBefore, s.state)
				}
				state = s.state
			}
		}
	})
}
