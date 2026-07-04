package vterm

import "testing"

// TestAdvanceParserCarryState_ModeTransitions walks the carry-state machine one
// byte at a time and asserts the resulting mode after each driving byte. It
// covers every documented edge between the carry modes (text, escape, CSI,
// CSI-param, OSC, DCS, charset) plus the bytes that fall through and reset to
// text.
func TestAdvanceParserCarryState_ModeTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seed     ParserCarryState
		data     []byte
		wantMode ParserCarryMode
		wantUTF8 int
	}{
		{
			name:     "plain ascii stays in text",
			data:     []byte("hello world"),
			wantMode: ParserCarryText,
		},
		{
			name:     "lone ESC enters escape",
			data:     []byte{0x1b},
			wantMode: ParserCarryEscape,
		},
		{
			name:     "ESC then bracket enters CSI",
			data:     []byte{0x1b, '['},
			wantMode: ParserCarryCSI,
		},
		{
			name:     "ESC then close-bracket enters OSC",
			data:     []byte{0x1b, ']'},
			wantMode: ParserCarryOSC,
		},
		{
			name:     "ESC then P enters DCS",
			data:     []byte{0x1b, 'P'},
			wantMode: ParserCarryDCS,
		},
		{
			name:     "ESC then ( enters charset",
			data:     []byte{0x1b, '('},
			wantMode: ParserCarryCharset,
		},
		{
			name:     "ESC then ) enters charset",
			data:     []byte{0x1b, ')'},
			wantMode: ParserCarryCharset,
		},
		{
			name:     "ESC then unknown final returns to text",
			data:     []byte{0x1b, '7'},
			wantMode: ParserCarryText,
		},
		{
			name:     "CSI digit advances to CSI param",
			data:     []byte{0x1b, '[', '3'},
			wantMode: ParserCarryCSIParam,
		},
		{
			name:     "CSI private marker advances to CSI param",
			data:     []byte{0x1b, '[', '?'},
			wantMode: ParserCarryCSIParam,
		},
		{
			name:     "CSI intermediate byte advances to CSI param",
			data:     []byte{0x1b, '[', ' '},
			wantMode: ParserCarryCSIParam,
		},
		{
			name:     "CSI final letter completes back to text",
			data:     []byte{0x1b, '[', 'm'},
			wantMode: ParserCarryText,
		},
		{
			name:     "full SGR sequence completes to text",
			data:     []byte("\x1b[1;31m"),
			wantMode: ParserCarryText,
		},
		{
			name:     "CSI param then final completes to text",
			data:     []byte{0x1b, '[', '3', '8', ';', '5', 'm'},
			wantMode: ParserCarryText,
		},
		{
			name:     "CSI param then ESC restarts escape",
			data:     []byte{0x1b, '[', '3', 0x1b},
			wantMode: ParserCarryEscape,
		},
		{
			name:     "ESC inside bare CSI restarts escape",
			data:     []byte{0x1b, '[', 0x1b},
			wantMode: ParserCarryEscape,
		},
		{
			name:     "CSI param colon stays in param",
			data:     []byte{0x1b, '[', '3', '8', ':', '2'},
			wantMode: ParserCarryCSIParam,
		},
		{
			name:     "CSI param non-final junk resets to text",
			data:     []byte{0x1b, '[', '3', 0x07},
			wantMode: ParserCarryText,
		},
		{
			name:     "OSC stays open until BEL terminator",
			data:     []byte("\x1b]0;some title"),
			wantMode: ParserCarryOSC,
		},
		{
			name:     "OSC BEL terminator returns to text",
			data:     []byte("\x1b]0;title\x07"),
			wantMode: ParserCarryText,
		},
		{
			name:     "OSC ESC begins string terminator",
			data:     []byte{0x1b, ']', '0', ';', 't', 0x1b},
			wantMode: ParserCarryOSCEscape,
		},
		{
			name:     "OSC ST terminator returns to text",
			data:     []byte("\x1b]0;t\x1b\\"),
			wantMode: ParserCarryText,
		},
		{
			name:     "OSC ESC bracket aborts OSC and enters CSI",
			data:     []byte("\x1b]0;t\x1b["),
			wantMode: ParserCarryCSI,
		},
		{
			name:     "DCS swallows bytes until ESC",
			data:     []byte("\x1bPq#0;2;0;0;0"),
			wantMode: ParserCarryDCS,
		},
		{
			name:     "DCS ESC begins string terminator",
			data:     []byte{0x1b, 'P', 'q', 0x1b},
			wantMode: ParserCarryDCSEscape,
		},
		{
			name:     "DCS ST terminator returns to text",
			data:     []byte("\x1bPq\x1b\\"),
			wantMode: ParserCarryText,
		},
		{
			name:     "charset designation byte returns to text",
			data:     []byte{0x1b, '(', 'B'},
			wantMode: ParserCarryText,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := AdvanceParserCarryState(tc.seed, tc.data)
			if got.Mode != tc.wantMode {
				t.Fatalf("AdvanceParserCarryState mode = %d, want %d", got.Mode, tc.wantMode)
			}
			if got.UTF8Remaining != tc.wantUTF8 {
				t.Fatalf("AdvanceParserCarryState UTF8Remaining = %d, want %d",
					got.UTF8Remaining, tc.wantUTF8)
			}
		})
	}
}

// TestAdvanceParserCarryState_UTF8Continuation covers the multi-byte UTF-8
// bookkeeping: a lead byte sets the expected continuation count, valid
// continuation bytes decrement it, and a non-continuation byte abandons the
// partial sequence and is reprocessed as a fresh lead.
func TestAdvanceParserCarryState_UTF8Continuation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seed     ParserCarryState
		data     []byte
		wantMode ParserCarryMode
		wantUTF8 int
	}{
		{
			name:     "two-byte lead leaves one remaining",
			data:     []byte{0xC3}, // start of "Ã©"
			wantMode: ParserCarryText,
			wantUTF8: 1,
		},
		{
			name:     "two-byte lead plus continuation completes",
			data:     []byte{0xC3, 0xA9}, // "Ã©"
			wantMode: ParserCarryText,
			wantUTF8: 0,
		},
		{
			name:     "three-byte lead leaves two remaining",
			data:     []byte{0xE2}, // start of "â‚¬"
			wantMode: ParserCarryText,
			wantUTF8: 2,
		},
		{
			name:     "three-byte lead plus one continuation leaves one",
			data:     []byte{0xE2, 0x82},
			wantMode: ParserCarryText,
			wantUTF8: 1,
		},
		{
			name:     "three-byte char fully consumed",
			data:     []byte{0xE2, 0x82, 0xAC}, // "â‚¬"
			wantMode: ParserCarryText,
			wantUTF8: 0,
		},
		{
			name:     "four-byte lead leaves three remaining",
			data:     []byte{0xF0}, // emoji lead
			wantMode: ParserCarryText,
			wantUTF8: 3,
		},
		{
			name:     "four-byte char fully consumed",
			data:     []byte{0xF0, 0x9F, 0x98, 0x80}, // ðŸ˜€
			wantMode: ParserCarryText,
			wantUTF8: 0,
		},
		{
			name:     "seed mid-utf8 resumes counting",
			seed:     ParserCarryState{Mode: ParserCarryText, UTF8Remaining: 2},
			data:     []byte{0x82, 0xAC}, // remaining two of "â‚¬"
			wantMode: ParserCarryText,
			wantUTF8: 0,
		},
		{
			name:     "non-continuation abandons partial and is reprocessed",
			data:     []byte{0xE2, 'A'}, // lead then ASCII 'A'
			wantMode: ParserCarryText,
			wantUTF8: 0,
		},
		{
			name:     "ESC after lead abandons utf8 and enters escape",
			data:     []byte{0xC3, 0x1b}, // lead then ESC
			wantMode: ParserCarryEscape,
			wantUTF8: 0,
		},
		{
			name:     "utf8 bytes are not parsed as control while remaining",
			seed:     ParserCarryState{Mode: ParserCarryText, UTF8Remaining: 1},
			data:     []byte{0xAC}, // a continuation byte, not a control
			wantMode: ParserCarryText,
			wantUTF8: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := AdvanceParserCarryState(tc.seed, tc.data)
			if got.Mode != tc.wantMode {
				t.Fatalf("mode = %d, want %d", got.Mode, tc.wantMode)
			}
			if got.UTF8Remaining != tc.wantUTF8 {
				t.Fatalf("UTF8Remaining = %d, want %d", got.UTF8Remaining, tc.wantUTF8)
			}
		})
	}
}

// TestAdvanceParserCarryState_EmptyAndNil verifies the function is a pure
// identity on empty/nil input: it returns the seed unchanged.
func TestAdvanceParserCarryState_EmptyAndNil(t *testing.T) {
	t.Parallel()

	seeds := []ParserCarryState{
		{Mode: ParserCarryText},
		{Mode: ParserCarryEscape},
		{Mode: ParserCarryCSIParam},
		{Mode: ParserCarryOSC},
		{Mode: ParserCarryOSCEscape},
		{Mode: ParserCarryDCS},
		{Mode: ParserCarryDCSEscape},
		{Mode: ParserCarryText, UTF8Remaining: 2},
	}

	for _, seed := range seeds {
		if got := AdvanceParserCarryState(seed, nil); got != seed {
			t.Fatalf("nil input changed seed %+v -> %+v", seed, got)
		}
		if got := AdvanceParserCarryState(seed, []byte{}); got != seed {
			t.Fatalf("empty input changed seed %+v -> %+v", seed, got)
		}
	}
}

// TestAdvanceParserCarryState_ChunkSplitInvariant proves the carry contract that
// the function exists to uphold: feeding a stream in any split must yield the
// same final state as feeding it whole, because the previous chunk's result is
// the next chunk's seed.
func TestAdvanceParserCarryState_ChunkSplitInvariant(t *testing.T) {
	t.Parallel()

	streams := [][]byte{
		[]byte("plain text only"),
		[]byte("\x1b[1;31mred\x1b[0m and back"),
		[]byte("title \x1b]0;hello\x07 done"),
		[]byte("\x1bPqsixel-data\x1b\\after"),
		[]byte("emoji \xF0\x9F\x98\x80 and \xC3\xA9 accent"),
		{0x1b, '[', '?', '2', '0', '4', '9', 'h'},
		{0x1b, '(', 'B', 'X'},
	}

	for si, stream := range streams {
		whole := AdvanceParserCarryState(ParserCarryState{}, stream)
		for split := 0; split <= len(stream); split++ {
			mid := AdvanceParserCarryState(ParserCarryState{}, stream[:split])
			got := AdvanceParserCarryState(mid, stream[split:])
			if got != whole {
				t.Fatalf("stream %d split at %d: chunked=%+v whole=%+v",
					si, split, got, whole)
			}
		}
	}
}

// TestAdvanceParserCarryState_AgreesWithLiveParser cross-checks the standalone
// carry model against the real Parser's CarryState() for the same input, which
// is the consistency guarantee the doc comment promises.
func TestAdvanceParserCarryState_AgreesWithLiveParser(t *testing.T) {
	t.Parallel()

	inputs := [][]byte{
		[]byte("hello"),
		{0x1b},
		{0x1b, '['},
		[]byte("\x1b[3"),
		[]byte("\x1b[38;5"),
		[]byte("\x1b]0;t"),
		[]byte("\x1b]0;t\x1b"),
		{0x1b, 'P', 'q'},
		[]byte("\x1bPq\x1b"),
		{0x1b, '('},
		{0xE2, 0x82}, // partial 3-byte utf8
		{0xF0},       // 4-byte lead only
	}

	for _, in := range inputs {
		want := AdvanceParserCarryState(ParserCarryState{}, in)

		vt := New(80, 24)
		vt.Write(in)
		got := vt.ParserCarryState()

		if got.Mode != want.Mode {
			t.Fatalf("input %q: live Mode=%d, model Mode=%d", in, got.Mode, want.Mode)
		}
		if got.UTF8Remaining != want.UTF8Remaining {
			t.Fatalf("input %q: live UTF8Remaining=%d, model UTF8Remaining=%d",
				in, got.UTF8Remaining, want.UTF8Remaining)
		}
	}
}

// TestExecuteOSC_SwallowsWithoutMutatingScreen confirms executeOSC is a no-op on
// terminal state: an OSC title sequence (BEL-terminated) leaves the screen and
// cursor untouched and the parser back in the ground state, ready for normal
// text.
func TestExecuteOSC_SwallowsWithoutMutatingScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		osc  []byte
	}{
		{name: "empty osc", osc: []byte("\x1b]\x07")},
		{name: "set window title", osc: []byte("\x1b]0;my window title\x07")},
		{name: "set foreground color", osc: []byte("\x1b]10;rgb:ff/00/00\x07")},
		{name: "osc with embedded semicolons", osc: []byte("\x1b]52;c;Zm9v\x07")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(20, 3)
			startVersion := vt.Version()

			vt.Write(tc.osc)
			// Following printable text must land at home, proving the OSC was
			// fully consumed and the cursor never moved.
			vt.Write([]byte("OK"))

			if got := vt.Screen[0][0].Rune; got != 'O' {
				t.Fatalf("after OSC, col 0 = %q, want 'O'", got)
			}
			if got := vt.Screen[0][1].Rune; got != 'K' {
				t.Fatalf("after OSC, col 1 = %q, want 'K'", got)
			}
			if vt.CursorY != 0 {
				t.Fatalf("OSC moved cursor row to %d, want 0", vt.CursorY)
			}
			if vt.Version() == startVersion {
				t.Fatalf("expected version bump from the trailing text write")
			}
		})
	}
}

// TestExecuteOSC_DirectCallIsInert exercises executeOSC directly to show it does
// not touch the VTerm regardless of what is buffered in oscBuf.
func TestExecuteOSC_DirectCallIsInert(t *testing.T) {
	t.Parallel()
	vt := New(10, 2)
	p := NewParser(vt)
	p.oscBuf.WriteString("0;arbitrary buffered payload")

	before := vt.Version()
	beforeRow := lineText(vt.Screen[0])

	p.executeOSC()

	if vt.Version() != before {
		t.Fatalf("executeOSC bumped version %d -> %d", before, vt.Version())
	}
	if got := lineText(vt.Screen[0]); got != beforeRow {
		t.Fatalf("executeOSC mutated screen: %q -> %q", beforeRow, got)
	}
}

// TestParseDCS_SwallowsUntilST drives parseDCS byte by byte and asserts it
// stays in the DCS state while consuming arbitrary payload bytes and only
// transitions to the escape-pending state when it sees ESC (the start of the ST
// terminator).
func TestParseDCS_SwallowsUntilST(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		payload   []byte
		wantState parseState
	}{
		{name: "stays in dcs on letters", payload: []byte("qabc"), wantState: stateDCS},
		{name: "stays in dcs on digits and semicolons", payload: []byte("0;1;2"), wantState: stateDCS},
		{name: "stays in dcs on control bytes other than esc", payload: []byte{0x07, 0x0a, 0x0d}, wantState: stateDCS},
		{name: "esc transitions to escape-pending", payload: []byte{'q', 0x1b}, wantState: stateDCSEscape},
		{name: "esc as first byte transitions to escape-pending", payload: []byte{0x1b}, wantState: stateDCSEscape},
		{name: "non-ST byte after esc resumes DCS", payload: []byte{'x', 0x1b, '['}, wantState: stateDCS},
		{name: "ST terminator returns to ground", payload: []byte{'x', 0x1b, '\\'}, wantState: stateGround},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, 2)
			p := NewParser(vt)
			p.state = stateDCS

			for _, b := range tc.payload {
				p.parseByte(b)
			}

			if p.state != tc.wantState {
				t.Fatalf("after DCS payload, state = %d, want %d", p.state, tc.wantState)
			}
		})
	}
}

// TestParseDCS_FullSequenceLeavesScreenClean verifies an end-to-end DCS string
// (DECRQSS-style, ESC P ... ESC \) is swallowed by Write without printing any of
// its bytes, and that subsequent text renders normally.
func TestParseDCS_FullSequenceLeavesScreenClean(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)

	// ESC P 1 $ r 0 m ESC \  (a typical DECRQSS reply form) followed by text.
	vt.Write([]byte("\x1bP1$r0m\x1b\\visible"))

	if got := lineText(vt.Screen[0]); got != "visible" {
		t.Fatalf("DCS not fully swallowed: row 0 = %q, want %q", got, "visible")
	}
}
