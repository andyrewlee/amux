package activity

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// TestOwnerLeaseAlive exercises the forward-skew/TTL window that decides whether
// a cross-instance activity-scan owner lease is still live: an empty owner or
// zero heartbeat is never alive, a heartbeat within OwnerLeaseTTL of the past is
// alive, and a future heartbeat is alive only inside OwnerFutureSkewTolerance.
func TestOwnerLeaseAlive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name  string
		lease OwnerLease
		want  bool
	}{
		{
			name:  "empty owner id rejected",
			lease: OwnerLease{OwnerID: "", HeartbeatAt: now},
			want:  false,
		},
		{
			name:  "whitespace owner id rejected",
			lease: OwnerLease{OwnerID: "   \t ", HeartbeatAt: now},
			want:  false,
		},
		{
			name:  "zero heartbeat rejected",
			lease: OwnerLease{OwnerID: "owner-1"},
			want:  false,
		},
		{
			name:  "recent heartbeat alive",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(-3 * time.Second)},
			want:  true,
		},
		{
			name:  "heartbeat exactly at TTL boundary alive",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(-OwnerLeaseTTL)},
			want:  true,
		},
		{
			name:  "heartbeat just past TTL expired",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(-OwnerLeaseTTL - time.Millisecond)},
			want:  false,
		},
		{
			name:  "heartbeat exactly now alive",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now},
			want:  true,
		},
		{
			name:  "small forward skew alive",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(time.Second)},
			want:  true,
		},
		{
			name:  "forward skew at tolerance boundary alive",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(OwnerFutureSkewTolerance)},
			want:  true,
		},
		{
			name:  "forward skew just past tolerance expired",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(OwnerFutureSkewTolerance + time.Millisecond)},
			want:  false,
		},
		{
			name:  "far future heartbeat expired",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(time.Hour)},
			want:  false,
		},
		{
			name:  "epoch does not affect liveness",
			lease: OwnerLease{OwnerID: "owner-1", HeartbeatAt: now.Add(-time.Second), Epoch: 99},
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OwnerLeaseAlive(tc.lease, now); got != tc.want {
				t.Fatalf("OwnerLeaseAlive(%+v, %v) = %v, want %v", tc.lease, now, got, tc.want)
			}
		})
	}
}

// TestEncodeSnapshot_DeterministicAndFilters confirms EncodeSnapshot sorts IDs,
// drops inactive and blank entries, normalizes a sub-1 epoch to 1, and emits the
// epoch;timestamp;j:<base64-json> shape that DecodeSnapshot round-trips.
func TestEncodeSnapshot_DeterministicAndFilters(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	encoded := EncodeSnapshot(map[string]bool{
		"ws-c":      true,
		"ws-a":      true,
		"ws-b":      true,
		"ws-off":    false,
		"  ":        true,
		" ws-trim ": true,
	}, 0, now)

	// Sub-1 epoch is clamped to 1 and the timestamp is the millisecond clock.
	wantPrefix := "1;" + strconv.FormatInt(now.UnixMilli(), 10) + ";j:"
	if got := encoded[:len(wantPrefix)]; got != wantPrefix {
		t.Fatalf("encoded prefix = %q, want %q (full=%q)", got, wantPrefix, encoded)
	}

	// Assert the payload ordering directly so dropping sort.Strings fails here:
	// decode the base64 after the "j:" prefix and confirm the JSON array is the
	// trimmed, deterministically sorted id slice.
	payloadB64 := encoded[len(wantPrefix):]
	rawJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("decoding base64 payload %q: %v", payloadB64, err)
	}
	var gotIDs []string
	if err := json.Unmarshal(rawJSON, &gotIDs); err != nil {
		t.Fatalf("unmarshaling payload JSON %q: %v", rawJSON, err)
	}
	wantIDs := []string{"ws-a", "ws-b", "ws-c", "ws-trim"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("payload ids = %v, want sorted %v (full=%q)", gotIDs, wantIDs, encoded)
	}

	parsed, epoch, at, ok := DecodeSnapshot(encoded)
	if !ok {
		t.Fatal("expected EncodeSnapshot output to decode")
	}
	if epoch != 1 {
		t.Fatalf("expected clamped epoch 1, got %d", epoch)
	}
	if !at.Equal(now) {
		t.Fatalf("expected timestamp %v, got %v", now, at)
	}
	want := map[string]bool{"ws-a": true, "ws-b": true, "ws-c": true, "ws-trim": true}
	if len(parsed) != len(want) {
		t.Fatalf("expected %d active ids, got %d (%v)", len(want), len(parsed), parsed)
	}
	for id := range want {
		if !parsed[id] {
			t.Fatalf("expected %q active in %v", id, parsed)
		}
	}
	if parsed["ws-off"] || parsed["  "] {
		t.Fatalf("inactive/blank ids must be excluded, got %v", parsed)
	}
}

// TestEncodeSnapshot_Empty round-trips an empty active set: a decodable snapshot
// with an empty (non-nil) map.
func TestEncodeSnapshot_Empty(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	encoded := EncodeSnapshot(map[string]bool{}, 5, now)

	parsed, epoch, _, ok := DecodeSnapshot(encoded)
	if !ok {
		t.Fatal("expected empty snapshot to decode")
	}
	if epoch != 5 {
		t.Fatalf("expected epoch 5, got %d", epoch)
	}
	if len(parsed) != 0 {
		t.Fatalf("expected empty active set, got %v", parsed)
	}
}

// TestDecodeSnapshot_Malformed covers the error paths that make DecodeSnapshot
// report ok=false: empty input, wrong field count, and non-numeric / non-positive
// epoch and timestamp fields.
func TestDecodeSnapshot_Malformed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"too few fields", "1;1700000000000"},
		{"single field", "1"},
		{"non-numeric epoch", "abc;1700000000000;"},
		{"zero epoch", "0;1700000000000;"},
		{"negative epoch", "-1;1700000000000;"},
		{"non-numeric timestamp", "1;notanumber;"},
		{"zero timestamp", "1;0;"},
		{"negative timestamp", "1;-5;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, epoch, at, ok := DecodeSnapshot(tc.raw)
			if ok {
				t.Fatalf("DecodeSnapshot(%q) = ok, want ok=false", tc.raw)
			}
			if parsed != nil {
				t.Fatalf("expected nil map on malformed input, got %v", parsed)
			}
			if epoch != 0 {
				t.Fatalf("expected zero epoch on malformed input, got %d", epoch)
			}
			if !at.IsZero() {
				t.Fatalf("expected zero time on malformed input, got %v", at)
			}
		})
	}
}

// TestDecodeSnapshot_PayloadFormats covers the empty payload, valid JSON payload,
// a JSON payload that fails base64/JSON decode (falls back to legacy parsing), and
// the legacy comma-delimited format including b:<base64> entries and blanks.
func TestDecodeSnapshot_PayloadFormats(t *testing.T) {
	const ts int64 = 1_700_000_000_000
	header := "2;" + strconv.FormatInt(ts, 10) + ";"

	jsonPayload := "j:" + base64.RawURLEncoding.EncodeToString(mustJSON(t, []string{"ws-a", "ws-b"}))
	// Valid base64 that decodes to a JSON object (not array) -> json payload fails,
	// then the raw "j:..." string is treated as a single legacy plain ID.
	notArray := "j:" + base64.RawURLEncoding.EncodeToString(mustJSON(t, map[string]int{"x": 1}))
	legacyB := "b:" + base64.RawURLEncoding.EncodeToString([]byte("ws-legacy"))

	cases := []struct {
		name string
		raw  string
		want map[string]bool
	}{
		{
			name: "empty payload yields empty set",
			raw:  header,
			want: map[string]bool{},
		},
		{
			name: "whitespace payload yields empty set",
			raw:  header + "   ",
			want: map[string]bool{},
		},
		{
			name: "json payload decoded",
			raw:  header + jsonPayload,
			want: map[string]bool{"ws-a": true, "ws-b": true},
		},
		{
			name: "json payload that is not an array falls back to legacy plain id",
			raw:  header + notArray,
			want: map[string]bool{notArray: true},
		},
		{
			name: "legacy comma-delimited plain ids",
			raw:  header + "ws-1, ws-2 ,ws-3",
			want: map[string]bool{"ws-1": true, "ws-2": true, "ws-3": true},
		},
		{
			name: "legacy base64-encoded id decoded",
			raw:  header + legacyB,
			want: map[string]bool{"ws-legacy": true},
		},
		{
			name: "legacy mix of plain, base64, and blanks",
			raw:  header + "ws-plain, ," + legacyB + ", ",
			want: map[string]bool{"ws-plain": true, "ws-legacy": true},
		},
		{
			name: "legacy b: with invalid base64 kept verbatim",
			raw:  header + "b:!!!not-base64!!!",
			want: map[string]bool{"b:!!!not-base64!!!": true},
		},
		{
			name: "legacy b: decoding to blank kept verbatim",
			raw:  header + "b:" + base64.RawURLEncoding.EncodeToString([]byte("   ")),
			want: map[string]bool{"b:" + base64.RawURLEncoding.EncodeToString([]byte("   ")): true},
		},
		{
			name: "legacy payload of only blanks yields empty set",
			raw:  header + " , ,  ",
			want: map[string]bool{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, epoch, at, ok := DecodeSnapshot(tc.raw)
			if !ok {
				t.Fatalf("DecodeSnapshot(%q) = ok=false, want ok=true", tc.raw)
			}
			if epoch != 2 {
				t.Fatalf("expected epoch 2, got %d", epoch)
			}
			if at.UnixMilli() != ts {
				t.Fatalf("expected timestamp %d, got %d", ts, at.UnixMilli())
			}
			if len(parsed) != len(tc.want) {
				t.Fatalf("active set size = %d, want %d (%v)", len(parsed), len(tc.want), parsed)
			}
			for id := range tc.want {
				if !parsed[id] {
					t.Fatalf("expected %q active in %v", id, parsed)
				}
			}
		})
	}
}

// TestDecodeSnapshotJSONPayload trims whitespace inside JSON entries, drops blank
// entries, and reports ok=false when the base64 or JSON decode fails.
func TestDecodeSnapshotJSONPayload(t *testing.T) {
	t.Run("trims and drops blanks", func(t *testing.T) {
		payload := "j:" + base64.RawURLEncoding.EncodeToString(mustJSON(t, []string{" ws-a ", "", "   ", "ws-b"}))
		parsed, ok := decodeSnapshotJSONPayload(payload)
		if !ok {
			t.Fatal("expected ok=true")
		}
		want := map[string]bool{"ws-a": true, "ws-b": true}
		if len(parsed) != len(want) {
			t.Fatalf("size = %d, want %d (%v)", len(parsed), len(want), parsed)
		}
		for id := range want {
			if !parsed[id] {
				t.Fatalf("expected %q in %v", id, parsed)
			}
		}
	})

	t.Run("invalid base64 rejected", func(t *testing.T) {
		if parsed, ok := decodeSnapshotJSONPayload("j:!!!"); ok || parsed != nil {
			t.Fatalf("expected ok=false/nil, got %v ok=%v", parsed, ok)
		}
	})

	t.Run("valid base64 but invalid json rejected", func(t *testing.T) {
		payload := "j:" + base64.RawURLEncoding.EncodeToString([]byte("not-json"))
		if parsed, ok := decodeSnapshotJSONPayload(payload); ok || parsed != nil {
			t.Fatalf("expected ok=false/nil, got %v ok=%v", parsed, ok)
		}
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%v): %v", v, err)
	}
	return b
}

// TestSnapshotFresh covers the forward-skew cap that keeps a future-dated
// snapshot from pinning followers to a stale active set forever.
func TestSnapshotFresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"far future rejected", now.Add(time.Minute), false},
		{"small forward skew accepted", now.Add(time.Second), true},
		{"recent past accepted", now.Add(-5 * time.Second), true},
		{"stale past rejected", now.Add(-15 * time.Second), false},
		{"exactly now accepted", now, true},
		{"future at skew boundary accepted", now.Add(SnapshotFutureSkewTolerance), true},
		{"future just past boundary rejected", now.Add(SnapshotFutureSkewTolerance + time.Millisecond), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotFresh(tc.at, now); got != tc.want {
				t.Fatalf("snapshotFresh(%v, %v) = %v, want %v", tc.at, now, got, tc.want)
			}
		})
	}
}

// TestSnapshotFresh_RoundTripThroughEncodeDecode drives a far-future timestamp
// through EncodeSnapshot/DecodeSnapshot to confirm the decoded timestamp still
// trips the skew cap (no loss of precision flips the freshness decision).
func TestSnapshotFresh_RoundTripThroughEncodeDecode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	future := now.Add(time.Minute)

	encoded := EncodeSnapshot(map[string]bool{"ws-a": true}, 3, future)
	parsed, epoch, at, ok := DecodeSnapshot(encoded)
	if !ok {
		t.Fatal("expected snapshot to decode")
	}
	if epoch != 3 {
		t.Fatalf("expected epoch 3, got %d", epoch)
	}
	if !parsed["ws-a"] {
		t.Fatalf("expected ws-a in decoded set, got %v", parsed)
	}
	if snapshotFresh(at, now) {
		t.Fatalf("far-future decoded timestamp %v must be rejected as stale", at)
	}
}
