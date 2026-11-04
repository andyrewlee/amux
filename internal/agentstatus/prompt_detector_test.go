package agentstatus

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// TestPromptMachineFormallyChecked verifies the detector table's structural
// properties: every state reachable, emission only on the two completing
// edges (BEL after A, ST after A's ESC look-ahead), and — the property that
// keeps junk prefixes like "]3133;A" from false-positive — every declared
// edge out of a non-ground state either advances the match or drops to
// ground.
func TestPromptMachineFormallyChecked(t *testing.T) {
	alphabet := []promptEvent{
		pmEscByte, pmRBracket, pmOne, pmThree, pmSemicolon,
		pmLetterA, pmLetterOther, pmTerminator, pmBackslash, pmOther,
	}
	err := promptMachine.Check(fsm.Checker[promptState, promptEvent]{
		Alphabet:             alphabet,
		AllDeclaredReachable: true,
		Edge: func(from promptState, ev promptEvent, to promptState) error {
			if promptEmitting(from, ev) && to != pmGround {
				return fmt.Errorf("an emission edge must land in ground (got %v --%v--> %v)", from, ev, to)
			}
			if from != pmGround && to == from &&
				!(from == pmEsc && ev == pmEscByte) {
				return fmt.Errorf("non-ground state %v must advance or drop, not stay", from)
			}
			if from == pmLetter && ev == pmTerminator && !promptEmitting(from, ev) {
				return errors.New("BEL after A must be an emission edge")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("prompt machine check failed: %v", err)
	}
	// Check only visits DECLARED edges, so the table's core invariant —
	// every (non-ground, event) pair is declared and drops or advances —
	// needs its own sweep: an undeclared pair is a silent STAY, and a stay
	// in a matching state lets junk like "\x1b]133;;A BEL" false-positive.
	// (The scanner's model test has the same class of Next assertions; this
	// mirrors them for the prompt machine. pmEsc keeps its ESC self-loop.)
	for _, s := range []promptState{pmEsc, pmOsc, pm1, pm3a, pm3b, pmSemi, pmLetter, pmLetterEsc} {
		for _, ev := range alphabet {
			_, ok := promptMachine.Next(s, ev)
			if !ok && !(s == pmEsc && ev == pmEscByte) {
				t.Fatalf("undeclared pair (%v, %v): a non-ground state must never stay", s, ev)
			}
		}
	}
}

// TestPromptDetectorRecognizesPromptMarks covers the positive and negative
// behavior cases, including chunk splits.
func TestPromptDetectorRecognizesPromptMarks(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"BEL terminated", "\x1b]133;A\x07", true},
		{"ST terminated", "\x1b]133;A\x1b\\", true},
		{"section C is not a prompt", "\x1b]133;C\x07", false},
		{"OSC 0 title", "\x1b]0;prompt\x07", false},
		{"OSC 7 cwd", "\x1b]7;file://x\x07", false},
		{"prefix junk does not anchor", "]3133;A\x07", false},
		{"prefix junk does not anchor (2)", "]1133;A\x07", false},
		{"doubled semicolon is not a section marker", "\x1b]133;;A\x07", false},
		{"plain text", "133;A run this", false},
		{"unterminated", "\x1b]133;A", false},
		{"two prompts in one chunk", "\x1b]133;A\x07\x1b]133;A\x07", true},
		{"prompt after other output", "build ok\r\n\x1b]133;A\x07$ ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &PromptDetector{}
			if got := d.Feed([]byte(tc.in)); got != tc.want {
				t.Fatalf("Feed(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestPromptDetectorSurvivesChunkSplits feeds one prompt mark split at every
// boundary — pane output arrives in arbitrary slices.
func TestPromptDetectorSurvivesChunkSplits(t *testing.T) {
	const input = "\x1b]133;A\x07"
	for i := 1; i < len(input)-1; i++ {
		d := &PromptDetector{}
		got := d.Feed([]byte(input[:i]))
		got = d.Feed([]byte(input[i:])) || got
		if !got {
			t.Fatalf("split at %d lost the prompt mark", i)
		}
	}
}

// TestPromptDetectorMatchesModel is the differential harness: every byte
// string over a small alphabet must agree with the naive model ("the input
// contains a complete prompt mark"). Brute-force length keeps the
// enumeration tractable while covering every interleaving the alphabet can
// express.
func TestPromptDetectorMatchesModel(t *testing.T) {
	alphabet := []string{"\x1b", "]", "1", "3", ";", "A", "C", "\x07", "\x1b\\", "x"}
	var build func(n int, prefix string, visit func(string))
	build = func(n int, prefix string, visit func(string)) {
		if n == 0 {
			visit(prefix)
			return
		}
		for _, a := range alphabet {
			build(n-1, prefix+a, visit)
		}
	}
	const maxLen = 4 // 10^4 strings: full coverage of every 4-byte shape
	build(maxLen, "", func(s string) {
		want := strings.Contains(s, "\x1b]133;A\x07") || strings.Contains(s, "\x1b]133;A\x1b\\")
		d := &PromptDetector{}
		// Feed byte-at-a-time: the harshest chunking.
		var got bool
		for i := 0; i < len(s); i++ {
			if d.Feed([]byte{s[i]}) {
				got = true
			}
		}
		if got != want {
			t.Fatalf("input %q: detector=%v model=%v", s, got, want)
		}
	})
}

// TestPromptDetectorRandomStreamsDifferential takes the differential past the
// brute-force length: random noise around planted prompt marks (the real
// sequence is 8 bytes; the enumeration above stops at 4), fed in random
// chunk sizes, must agree with the naive contains-model. Deterministic seed.
func TestPromptDetectorRandomStreamsDifferential(t *testing.T) {
	const noiseAlphabet = "\x1b]13;AC\x07\\abc$ "
	rng := rand.New(rand.NewSource(7501))
	marks := []string{"\x1b]133;A\x07", "\x1b]133;A\x1b\\"}
	nearMisses := []string{"\x1b]133;C\x07", "\x1b]1333;A\x07", "\x1b]1133;A\x07", "]133;A\x07", "\x1b]133;A", "\x1b]133;;A\x07"}
	for i := 0; i < 2000; i++ {
		var b strings.Builder
		pieces := rng.Intn(12)
		for p := 0; p < pieces; p++ {
			switch rng.Intn(3) {
			case 0: // planted real mark
				b.WriteString(marks[rng.Intn(len(marks))])
			case 1: // near-miss that must NOT match
				b.WriteString(nearMisses[rng.Intn(len(nearMisses))])
			default: // random noise
				n := rng.Intn(24)
				for j := 0; j < n; j++ {
					b.WriteByte(noiseAlphabet[rng.Intn(len(noiseAlphabet))])
				}
			}
		}
		stream := b.String()
		want := strings.Contains(stream, marks[0]) || strings.Contains(stream, marks[1])
		d := &PromptDetector{}
		var got bool
		for off := 0; off < len(stream); {
			n := 1 + rng.Intn(7) // arbitrary chunk boundaries
			end := off + n
			if end > len(stream) {
				end = len(stream)
			}
			if d.Feed([]byte(stream[off:end])) {
				got = true
			}
			off = end
		}
		if got != want {
			t.Fatalf("stream %q: detector=%v model=%v", stream, got, want)
		}
	}
}
