package vterm

import (
	"bytes"
	"fmt"
	"testing"
)

// captureResponses wires a ResponseWriter into the terminal that accumulates
// every byte the VTerm emits back toward the PTY, returning the buffer so tests
// can assert on the exact query responses produced by DSR/DECRQM.
func captureResponses(term *VTerm) *bytes.Buffer {
	buf := &bytes.Buffer{}
	term.SetResponseWriter(func(data []byte) {
		buf.Write(data)
	})
	return buf
}

func TestExecuteDSRDefaultParamZeroNoResponse(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// CSI n with no explicit parameter: the CSI parser substitutes the
	// default param 0, so executeDSR sees params == [0] and falls through
	// its switch (only 5 and 6 are handled), emitting nothing. The
	// len(params)==0 guard is unreachable via Write.
	term.Write([]byte("\x1b[n"))

	if buf.Len() != 0 {
		t.Fatalf("expected no response for DSR with default param 0, got %q", buf.String())
	}
}

func TestExecuteDSRStatusReport(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// DSR 5 -> device status report, terminal replies "OK" (CSI 0 n).
	term.Write([]byte("\x1b[5n"))

	if got, want := buf.String(), "\x1b[0n"; got != want {
		t.Fatalf("DSR 5 response = %q, want %q", got, want)
	}
}

func TestExecuteDSRCursorPositionReport(t *testing.T) {
	tests := []struct {
		name    string
		moveTo  string // CUP sequence positioning the cursor before the query
		wantRow int
		wantCol int
	}{
		{
			name:    "home position reports 1;1",
			moveTo:  "\x1b[1;1H",
			wantRow: 1,
			wantCol: 1,
		},
		{
			name:    "mid screen reports 1-indexed coordinates",
			moveTo:  "\x1b[10;20H",
			wantRow: 10,
			wantCol: 20,
		},
		{
			name:    "default CUP (no params) homes cursor",
			moveTo:  "\x1b[H",
			wantRow: 1,
			wantCol: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := New(80, 24)
			buf := captureResponses(term)

			term.Write([]byte(tt.moveTo))
			term.Write([]byte("\x1b[6n"))

			want := fmt.Sprintf("\x1b[%d;%dR", tt.wantRow, tt.wantCol)
			if got := buf.String(); got != want {
				t.Fatalf("DSR 6 response = %q, want %q (cursor at row=%d col=%d)",
					got, want, term.CursorY+1, term.CursorX+1)
			}
		})
	}
}

func TestExecuteDSRCursorReportReflectsCurrentPosition(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// Drive the cursor by writing printable text, then query position. After
	// "hello" (5 cells) the cursor sits at column 6 (1-indexed) on row 1.
	term.Write([]byte("hello"))
	term.Write([]byte("\x1b[6n"))

	if got, want := buf.String(), "\x1b[1;6R"; got != want {
		t.Fatalf("DSR 6 after typing 'hello' = %q, want %q", got, want)
	}
}

func TestExecuteDSRUnknownParamNoResponse(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// An unrecognized DSR parameter (e.g. 99) hits no case and produces no
	// reply.
	term.Write([]byte("\x1b[99n"))

	if buf.Len() != 0 {
		t.Fatalf("expected no response for unknown DSR param, got %q", buf.String())
	}
}

func TestExecuteDSRNilResponseWriterDoesNotPanic(t *testing.T) {
	term := New(80, 24)
	// No response writer installed: respond() must be a no-op, not a panic.
	term.Write([]byte("\x1b[5n"))
	term.Write([]byte("\x1b[6n"))
}

func TestExecuteDECRQMSynchronizedOutputInactive(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// DECRQM for mode 2026 while synchronized output is inactive -> status 2
	// ("reset / permanently set", here meaning currently reset).
	term.Write([]byte("\x1b[?2026$p"))

	if got, want := buf.String(), "\x1b[?2026;2$y"; got != want {
		t.Fatalf("DECRQM 2026 (inactive) response = %q, want %q", got, want)
	}
}

func TestExecuteDECRQMSynchronizedOutputActive(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// Begin synchronized output (DECSET 2026) so syncActive is true, then query.
	term.Write([]byte("\x1b[?2026h"))
	if !term.SyncActive() {
		t.Fatal("expected synchronized output to be active after DECSET 2026")
	}

	term.Write([]byte("\x1b[?2026$p"))

	if got, want := buf.String(), "\x1b[?2026;1$y"; got != want {
		t.Fatalf("DECRQM 2026 (active) response = %q, want %q", got, want)
	}
}

func TestExecuteDECRQMUnknownModeReportsNotRecognized(t *testing.T) {
	tests := []struct {
		name string
		mode int
	}{
		{name: "DECCKM 1", mode: 1},
		{name: "alt screen 1049", mode: 1049},
		{name: "bracketed paste 2004", mode: 2004},
		{name: "high mode number", mode: 9999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := New(80, 24)
			buf := captureResponses(term)

			term.Write([]byte(fmt.Sprintf("\x1b[?%d$p", tt.mode)))

			// Unrecognized modes report status 0 ("not recognized").
			want := fmt.Sprintf("\x1b[?%d;0$y", tt.mode)
			if got := buf.String(); got != want {
				t.Fatalf("DECRQM %d response = %q, want %q", tt.mode, got, want)
			}
		})
	}
}

func TestExecuteDECRQMMissingParamDefaultsToModeZero(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// DECRQM with no explicit parameter: the CSI parser substitutes a default
	// param of 0, so executeDECRQM treats it as mode 0 (unrecognized) and
	// reports status 0 rather than hitting the empty-params early return.
	term.Write([]byte("\x1b[?$p"))

	if got, want := buf.String(), "\x1b[?0;0$y"; got != want {
		t.Fatalf("DECRQM with missing param response = %q, want %q", got, want)
	}
}

func TestExecuteDECRQMMultipleParamsRespondPerMode(t *testing.T) {
	term := New(80, 24)
	buf := captureResponses(term)

	// Activate synchronized output so 2026 reports status 1 while the unknown
	// mode 1 reports status 0; a single multi-param DECRQM must emit one reply
	// per requested mode, in order.
	term.Write([]byte("\x1b[?2026h"))
	term.Write([]byte("\x1b[?2026;1$p"))

	if got, want := buf.String(), "\x1b[?2026;1$y\x1b[?1;0$y"; got != want {
		t.Fatalf("DECRQM multi-param response = %q, want %q", got, want)
	}
}

func TestExecuteDECRQMNilResponseWriterDoesNotPanic(t *testing.T) {
	term := New(80, 24)
	// No response writer: a DECRQM query must not panic.
	term.Write([]byte("\x1b[?2026$p"))
}

func TestMouseReportingModesTracked(t *testing.T) {
	term := New(80, 24)

	term.Write([]byte("\x1b[?1000h"))
	if !term.MouseReportingEnabled() {
		t.Fatal("expected normal mouse reporting to be enabled")
	}
	if term.MouseSGRMode() {
		t.Fatal("expected SGR mouse mode to start disabled")
	}

	term.Write([]byte("\x1b[?1006h"))
	if !term.MouseSGRMode() {
		t.Fatal("expected SGR mouse mode to be enabled")
	}

	term.Write([]byte("\x1b[?1000l"))
	if term.MouseReportingEnabled() {
		t.Fatal("expected mouse reporting to be disabled")
	}
	if !term.MouseSGRMode() {
		t.Fatal("expected SGR coordinate mode to remain tracked independently")
	}

	term.Write([]byte("\x1b[?1006l"))
	if term.MouseSGRMode() {
		t.Fatal("expected SGR mouse mode to be disabled")
	}
}

func TestMouseReportingModesClearOnTerminalReset(t *testing.T) {
	term := New(80, 24)
	term.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	if !term.MouseReportingEnabled() || !term.MouseSGRMode() {
		t.Fatal("expected mouse reporting modes to start enabled")
	}

	term.Write([]byte("\x1bc"))

	if term.MouseReportingEnabled() {
		t.Fatal("expected terminal reset to disable mouse reporting")
	}
	if term.MouseSGRMode() {
		t.Fatal("expected terminal reset to disable SGR mouse mode")
	}
}
