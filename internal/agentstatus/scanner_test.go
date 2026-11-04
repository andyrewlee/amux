package agentstatus

import (
	"slices"
	"strings"
	"testing"
)

func feedAll(t *testing.T, s *Scanner, chunks ...string) []string {
	t.Helper()
	var got []string
	for _, c := range chunks {
		got = append(got, s.Feed([]byte(c))...)
	}
	return got
}

func TestScannerExtractsBothTerminators(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []string
	}{
		{"BEL terminated", "\x1b]7501;state=blocked:app=a\x07", []string{"state=blocked:app=a"}},
		{"ST terminated", "\x1b]7501;state=working\x1b\\", []string{"state=working"}},
		{"two in a row", "\x1b]7501;a\x07text\x1b]7501;b\x1b\\", []string{"a", "b"}},
		{"surrounded by output", "line\r\n\x1b]7501;state=done\x07\r\nmore", []string{"state=done"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := feedAll(t, &Scanner{}, tc.input)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestScannerSurvivesChunkSplits feeds one sequence split at every possible
// boundary — the PTY delivers arbitrary slices, and a sequence assembled
// across reads must still land.
func TestScannerSurvivesChunkSplits(t *testing.T) {
	const input = "\x1b]7501;state=blocked:app=my-agent\x07"
	for i := 1; i < len(input)-1; i++ {
		s := &Scanner{}
		got := feedAll(t, s, input[:i], input[i:])
		if !slices.Equal(got, []string{"state=blocked:app=my-agent"}) {
			t.Fatalf("split at %d: got %q", i, got)
		}
	}
	// The same split via single-byte chunks.
	s := &Scanner{}
	var got []string
	for _, r := range input {
		got = append(got, s.Feed([]byte(string(r)))...)
	}
	if !slices.Equal(got, []string{"state=blocked:app=my-agent"}) {
		t.Fatalf("byte-at-a-time: got %q", got)
	}
}

func TestScannerIgnoresOtherSequences(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"title", "\x1b]0;my title\x07"},
		{"osc 7 cwd", "\x1b]7;file://host/tmp\x1b\\"},
		{"clipboard", "\x1b]52;c;aGk=\x07"},
		{"csi color", "\x1b[31mred\x1b[0m"},
		{"two byte escape then literal osc text", "\x1b(B]7501;state=blocked\x07"},
		{"escaped abort mid payload", "\x1b]7501;state=blocked\x1b(A\x07"},
		{"CAN aborts", "\x1b]7501;state=blocked\x18\x07"},
		{"plain text", "]7501;state=blocked\x07"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := feedAll(t, &Scanner{}, tc.input)
			if len(got) != 0 {
				t.Fatalf("unexpectedly extracted %q", got)
			}
		})
	}
}

// TestScannerEscapedAbortFollowedByRealReport pins the recovery path: an
// aborted sequence must leave the scanner in ground, ready for the next
// report.
func TestScannerEscapedAbortFollowedByRealReport(t *testing.T) {
	got := feedAll(t, &Scanner{}, "\x1b]7501;state=blocked\x1b(A\x07\x1b]7501;state=idle\x07")
	if !slices.Equal(got, []string{"state=idle"}) {
		t.Fatalf("got %q", got)
	}
}

// TestScannerPayloadOverflowDropsSequence pins the memory bound: an
// unterminated oversized sequence is discarded and the scanner keeps
// scanning after it.
func TestScannerPayloadOverflowDropsSequence(t *testing.T) {
	huge := "\x1b]7501;state=x" + strings.Repeat("a", MaxPayloadBytes+10)
	got := feedAll(t, &Scanner{}, huge+"\x1b]7501;state=idle\x07")
	if !slices.Equal(got, []string{"state=idle"}) {
		t.Fatalf("got %q, want only the post-overflow report", got)
	}
}

func TestParsePayload(t *testing.T) {
	msg := EncodeText("needs approval")
	title := EncodeText("US East")
	for _, tc := range []struct {
		name    string
		payload string
		want    Report
		ok      bool
	}{
		{
			"full", "state=blocked:kind=question:app=my-agent:msg=" + msg,
			Report{State: "blocked", Kind: "question", App: "my-agent", Message: "needs approval", Progress: progressAbsent},
			true,
		},
		{"state only", "state=idle", Report{State: "idle", Progress: progressAbsent}, true},
		{"clear", "state=clear", Report{State: "clear", Progress: progressAbsent}, true},
		{
			"id addresses a record", "state=working:id=us-east:app=deploy",
			Report{State: "working", ID: "us-east", App: "deploy", Progress: progressAbsent},
			true,
		},
		{"unknown keys ignored", "state=done:future=1:noise", Report{State: "done", Progress: progressAbsent}, true},
		{"empty value", "state=", Report{}, false},
		{"missing state", "app=x:msg=" + msg, Report{}, false},
		{"unknown state discards report", "state=vibrating:app=x", Report{}, false},
		{"garbage pairs skipped, rest parsed", "junk:state=error", Report{State: "error", Progress: progressAbsent}, true},
		{
			"pair with byte outside value grammar skipped", "state=working:msg=has space:app=x",
			Report{State: "working", App: "x", Progress: progressAbsent},
			true,
		},
		{"repeated key last wins", "state=working:state=done", Report{State: "done", Progress: progressAbsent}, true},
		{
			// A comma is inside the value grammar (so the pair is not
			// malformed) but outside the app grammar.
			"invalid repeated app clears the earlier value", "state=working:app=good:app=go,od",
			Report{State: "working", Progress: progressAbsent},
			true,
		},
		{
			"invalid repeated kind clears the earlier value", "state=blocked:kind=question:kind=musing",
			Report{State: "blocked", Progress: progressAbsent},
			true,
		},
		{
			"invalid repeated progress clears the earlier value", "state=working:progress=40:progress=140",
			Report{State: "working", Progress: progressAbsent},
			true,
		},
		{
			"kind with non-blocked state treated as absent", "state=working:kind=question",
			Report{State: "working", Progress: progressAbsent},
			true,
		},
		{"unknown kind treated as absent", "state=blocked:kind=musing", Report{State: "blocked", Progress: progressAbsent}, true},
		{
			"progress parsed", "state=working:progress=40",
			Report{State: "working", Progress: 40},
			true,
		},
		{
			"progress out of range treated as absent", "state=working:progress=140",
			Report{State: "working", Progress: progressAbsent},
			true,
		},
		{
			"progress with done treated as absent", "state=done:progress=10",
			Report{State: "done", Progress: progressAbsent},
			true,
		},
		{
			"title decoded", "state=working:id=us-east:title=" + title,
			Report{State: "working", ID: "us-east", Title: "US East", Progress: progressAbsent},
			true,
		},
		// base64 chars are all inside the value grammar, so "grammar-legal
		// but undecodable" needs an interior '=' or length error.
		{"msg bad base64 discards report", "state=blocked:msg=aa=a", Report{}, false},
		{"msg control rune discards report", "state=blocked:msg=" + EncodeText("line\nbreak"), Report{}, false},
		{"app outside grammar treated as absent", "state=working:app=has space", Report{State: "working", Progress: progressAbsent}, true},
		{"malformed id discards report", "state=working:id=/leading", Report{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePayload(tc.payload)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %#v)", ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestParsePayloadLimits pins the protocol's limits table: a report that
// breaks a size limit is discarded whole, never truncated into a lie.
func TestParsePayloadLimits(t *testing.T) {
	overMsg := strings.Repeat("a", MaxMsgEncoded+1)
	if _, ok := ParsePayload("state=blocked:msg=" + overMsg); ok {
		t.Fatal("over-length encoded msg must discard the report")
	}
	overTitle := strings.Repeat("a", MaxTitleEncoded+1)
	if _, ok := ParsePayload("state=working:title=" + overTitle); ok {
		t.Fatal("over-length encoded title must discard the report")
	}
	// A decoded msg within the cap survives untouched.
	text := strings.Repeat("m", MaxMsgDecoded-100)
	got, ok := ParsePayload("state=blocked:msg=" + EncodeText(text))
	if !ok || got.Message != text {
		t.Fatalf("in-limit message mangled: ok=%v len=%d", ok, len(got.Message))
	}
	// Padding is optional per the spec.
	padded := EncodeText("padded message") + "=="
	if got, ok := ParsePayload("state=done:msg=" + padded); !ok || got.Message != "padded message" {
		t.Fatalf("padded base64 rejected: ok=%v msg=%q", ok, got.Message)
	}
}

// TestEncodePayloadRoundTrip pins the CLI channel emitting byte-identical
// payloads to the OSC channel.
func TestEncodePayloadRoundTrip(t *testing.T) {
	r := Report{State: "blocked", Kind: "question", App: "my-agent", Message: "needs approval: now"}
	back, ok := ParsePayload(r.EncodePayload())
	if !ok || back != r {
		t.Fatalf("round trip mismatch: %#v -> %q -> %#v", r, r.EncodePayload(), back)
	}
}

// TestEncodePayloadGrammarConformance runs the encoder's output through the
// same value-grammar check the parser enforces — the emit side can never
// produce a byte the consume side rejects.
func TestEncodePayloadGrammarConformance(t *testing.T) {
	for _, r := range []Report{
		{State: StateIdle},
		{State: StateWorking, App: "brew", Progress: 40, Message: "Installing updates"},
		{State: StateBlocked, Kind: KindAuth, App: "brew", Message: "Password required"},
		{State: StateClear},
		{State: StateDone, App: "deploy", Title: "Deploy", Message: "done: 3 regions"},
	} {
		payload := r.EncodePayload()
		if _, ok := ParsePayload(payload); !ok {
			t.Fatalf("encoder produced a payload the parser rejects: %q (from %#v)", payload, r)
		}
	}
}
