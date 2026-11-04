package agentstatus

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"
)

// State values defined by the protocol. The set is closed: rev 0.4 says a
// report with an unrecognized state is ignored whole, so a state added in a
// later revision never silently degrades to idle on an older consumer.
const (
	StateIdle    = "idle"
	StateWorking = "working"
	StateDone    = "done"
	StateBlocked = "blocked"
	StateError   = "error"
	StateClear   = "clear" // not a state: removes records
)

// Kind values for blocked reports: what the program is waiting on.
const (
	KindPermission = "permission"
	KindQuestion   = "question"
	KindAuth       = "auth"
)

// Hard caps from the protocol's limits table. A report that breaks one is
// discarded whole. The scanner's payload cap (MaxPayloadBytes) equals the
// whole-sequence cap of 4096 bytes; since the largest legal report is under
// 3300 bytes, a payload cap of 4096 never discards a legal report.
const (
	MaxAppBytes       = 32
	MaxIDBytes        = 128
	MaxKeyBytes       = 16
	MaxMsgEncoded     = 2732
	MaxMsgDecoded     = 2048
	MaxTitleEncoded   = 256
	MaxTitleDecoded   = 192
	MaxRecordsPerPane = 64 // spec MUST support at least 64
	MaxPayloadBytes   = 4096
)

// Report is one parsed OSC 7501 report. State is the reported state label
// (one of the State* constants, never empty); ID addresses a record ("" is
// the root record); App, Kind, Message, Title, and Progress carry the
// optional keys, with Message and Title already base64-decoded to UTF-8
// text (empty when absent); AtMS is the Unix-milliseconds timestamp amux
// recorded at accept time — the protocol has no timestamps of its own.
// PaneDead is a consumer-side annotation, never on the wire: the tmux read
// marks reports from panes whose process has exited, and consumers enforce
// the protocol's process-exit lifetime (working/blocked records drop,
// done/error/idle survive). It rides on the read result — not recomputed
// per consumer — because `amux wait` reads pane options outside any
// activity scan and needs the same lifetime verdict as the dashboard.
type Report struct {
	State    string
	ID       string
	App      string
	Kind     string
	Message  string
	Title    string
	Progress int // -1 = absent
	AtMS     int64
	PaneDead bool
}

// IsClear reports whether r is a clear directive rather than a state.
func (r Report) IsClear() bool { return r.State == StateClear }

// progressAbsent is the Progress value for reports without progress.
const progressAbsent = -1

// validState is the closed state vocabulary.
func validState(s string) bool {
	switch s {
	case StateIdle, StateWorking, StateDone, StateBlocked, StateError, StateClear:
		return true
	}
	return false
}

// validKind is the closed kind vocabulary; unrecognized values are treated
// as absent.
func validKind(s string) bool {
	switch s {
	case KindPermission, KindQuestion, KindAuth:
		return true
	}
	return false
}

// appCharset matches the protocol's app grammar [A-Za-z0-9_.+-]{1,32}.
func validApp(s string) bool {
	if len(s) == 0 || len(s) > MaxAppBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return true
}

// validID matches the protocol's id grammar: 1–8 levels, each segment
// 1–32 bytes of [A-Za-z0-9_.+-], joined by '/'; 128 bytes total. A report
// whose id does not match is ignored — falling back to the root record
// would let a malformed id overwrite it.
func validID(s string) bool {
	if len(s) == 0 || len(s) > MaxIDBytes {
		return false
	}
	segments := strings.Split(s, "/")
	if len(segments) > 8 {
		return false
	}
	for _, seg := range segments {
		if !validApp(seg) { // same segment grammar as app
			return false
		}
	}
	return true
}

// valueByte reports whether b is inside the protocol's value grammar
// [A-Za-z0-9_.,+/=-]. The grammar excludes ':' (the pair separator) and
// ';' so nothing needs escaping; '/' appears in base64 and ids.
func valueByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_' || b == '.' || b == ',' || b == '+' || b == '/' || b == '=' || b == '-':
		return true
	}
	return false
}

// ParsePayload parses the body of one OSC 7501 sequence (everything
// between "7501;" and the terminator) per revision 0.4:
//
//   - colon-separated key=value pairs, nothing else;
//   - a malformed pair (no '=', empty key, over-length key, or a byte
//     outside the value grammar) is skipped and the rest is still parsed;
//   - unknown keys are ignored (this is how the protocol is extended);
//   - if a key repeats, the last value wins — and an out-of-grammar last
//     value clears the key (app, kind, progress) rather than resurrecting
//     an earlier valid one;
//   - msg and title are base64 of UTF-8; padding is optional; a value that
//     does not decode, or whose decoded text contains a control rune
//     (U+0000–U+001F, U+007F, U+0080–U+009F), discards the report whole;
//   - state is required and must be a known value, else the report is
//     ignored whole;
//   - kind and progress outside their grammars are treated as absent;
//   - a report that breaks a limit is discarded whole.
func ParsePayload(payload string) (Report, bool) {
	var r Report
	r.Progress = progressAbsent
	discard := false
	for _, pair := range strings.Split(payload, ":") {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" || len(key) > MaxKeyBytes {
			continue // malformed pair: skip, keep parsing
		}
		for i := 0; i < len(value); i++ {
			if !valueByte(value[i]) {
				// The byte is outside the value grammar, so this pair is
				// malformed: skip it (and not the report).
				value = ""
				break
			}
		}
		if value == "" {
			continue
		}
		switch key {
		case "state":
			if validState(value) {
				r.State = value
			} else {
				discard = true // unrecognized state: ignore the report
			}
		case "id":
			if validID(value) {
				r.ID = value
			} else {
				discard = true // malformed id: ignore, never touch root
			}
		case "app":
			if validApp(value) {
				r.App = value
			} else {
				r.App = "" // last wins: an invalid repeat clears the key
			} // outside grammar: treated as absent
		case "kind":
			if validKind(value) {
				r.Kind = value
			} else {
				r.Kind = "" // last wins: an invalid repeat clears the key
			} // unrecognized: treated as absent
		case "progress":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 100 {
				r.Progress = n
			} else {
				r.Progress = progressAbsent // last wins: an invalid repeat clears the key
			} // anything else: treated as absent
		case "msg":
			text, ok := decodeText(value, MaxMsgEncoded, MaxMsgDecoded)
			if !ok {
				discard = true // bad base64 or control runes: discard whole
			} else {
				r.Message = text
			}
		case "title":
			text, ok := decodeText(value, MaxTitleEncoded, MaxTitleDecoded)
			if !ok {
				discard = true
			} else {
				r.Title = text
			}
		}
	}
	if discard || r.State == "" {
		return Report{}, false
	}
	if r.State != StateBlocked {
		r.Kind = "" // kind is with blocked only, ignored otherwise
	}
	if r.State != StateWorking && r.State != StateBlocked {
		r.Progress = progressAbsent // progress is with working/blocked only
	}
	return r, true
}

// decodeText decodes a base64 free-text value: the encoded size is checked
// against encLimit before decoding, the decoded size against decLimit, and
// the decoded text must be valid UTF-8 without control runes. Padding is
// optional per the protocol.
func decodeText(value string, encLimit, decLimit int) (string, bool) {
	if len(value) > encLimit {
		return "", false
	}
	decoded, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(
		strings.TrimRight(value, "="))
	if err != nil {
		return "", false
	}
	if len(decoded) > decLimit {
		return "", false
	}
	if !utf8.Valid(decoded) {
		return "", false
	}
	for _, r := range string(decoded) {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return "", false
		}
	}
	return string(decoded), true
}

// EncodeText renders UTF-8 text as the protocol's base64 free-text form —
// the inverse of decodeText, used by `amux report` so the CLI emits
// byte-exact protocol sequences.
func EncodeText(text string) string {
	return base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(text))
}

// EncodePayload renders a Report back into wire form — the inverse of
// ParsePayload for the keys amux emits, used by the `amux report` CLI
// path so manual and scripted reports are byte-identical to what a
// spec-conformant program would print.
func (r Report) EncodePayload() string {
	var b strings.Builder
	b.WriteString("state=")
	b.WriteString(r.State)
	if r.ID != "" {
		b.WriteString(":id=")
		b.WriteString(r.ID)
	}
	if r.App != "" {
		b.WriteString(":app=")
		b.WriteString(r.App)
	}
	if r.Kind != "" && r.State == StateBlocked {
		b.WriteString(":kind=")
		b.WriteString(r.Kind)
	}
	if r.Progress >= 0 && (r.State == StateWorking || r.State == StateBlocked) {
		b.WriteString(":progress=")
		b.WriteString(strconv.Itoa(r.Progress))
	}
	if r.Title != "" {
		b.WriteString(":title=")
		b.WriteString(EncodeText(r.Title))
	}
	if r.Message != "" {
		b.WriteString(":msg=")
		b.WriteString(EncodeText(r.Message))
	}
	return b.String()
}

// Storage sanitization is the tmux layer's job (sanitizeTagValue): storage
// formats are a tmux concern, and this package's values are already
// grammar-checked at parse time.
