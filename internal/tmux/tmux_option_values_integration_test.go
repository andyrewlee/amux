package tmux

import (
	"testing"
	"time"
)

func TestSetSessionTagValues_SetsSessionOptions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "tag-write-batch", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	timestamp := "1700000000123"
	if err := SetSessionTagValues("tag-write-batch", []OptionValue{
		{Key: TagLastOutputAt, Value: timestamp},
		{Key: TagSessionLeaseAt, Value: timestamp},
	}, opts); err != nil {
		t.Fatalf("SetSessionTagValues: %v", err)
	}

	gotOutput, err := SessionTagValue("tag-write-batch", TagLastOutputAt, opts)
	if err != nil {
		t.Fatalf("SessionTagValue last output: %v", err)
	}
	if gotOutput != timestamp {
		t.Fatalf("expected %s=%q, got %q", TagLastOutputAt, timestamp, gotOutput)
	}

	gotLease, err := SessionTagValue("tag-write-batch", TagSessionLeaseAt, opts)
	if err != nil {
		t.Fatalf("SessionTagValue lease: %v", err)
	}
	if gotLease != timestamp {
		t.Fatalf("expected %s=%q, got %q", TagSessionLeaseAt, timestamp, gotLease)
	}
}

func TestSetGlobalOptionValues_SetsGlobalOptions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	if err := SetGlobalOptionValues([]OptionValue{
		{Key: "@amux_batch_opt_a", Value: "a"},
		{Key: "@amux_batch_opt_b", Value: "b"},
	}, opts); err != nil {
		t.Fatalf("SetGlobalOptionValues: %v", err)
	}

	gotA, err := GlobalOptionValue("@amux_batch_opt_a", opts)
	if err != nil {
		t.Fatalf("GlobalOptionValue @amux_batch_opt_a: %v", err)
	}
	if gotA != "a" {
		t.Fatalf("expected @amux_batch_opt_a=%q, got %q", "a", gotA)
	}

	gotB, err := GlobalOptionValue("@amux_batch_opt_b", opts)
	if err != nil {
		t.Fatalf("GlobalOptionValue @amux_batch_opt_b: %v", err)
	}
	if gotB != "b" {
		t.Fatalf("expected @amux_batch_opt_b=%q, got %q", "b", gotB)
	}
}

func TestGlobalOptionValues_ReadsMultipleOptions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	if err := SetGlobalOptionValues([]OptionValue{
		{Key: "@amux_batch_read_a", Value: "read-a"},
		{Key: "@amux_batch_read_b", Value: "read-b"},
	}, opts); err != nil {
		t.Fatalf("SetGlobalOptionValues: %v", err)
	}

	values, err := GlobalOptionValues([]string{
		"@amux_batch_read_a",
		"@amux_batch_read_b",
		"@amux_batch_read_missing",
	}, opts)
	if err != nil {
		t.Fatalf("GlobalOptionValues: %v", err)
	}
	if got := values["@amux_batch_read_a"]; got != "read-a" {
		t.Fatalf("expected @amux_batch_read_a=%q, got %q", "read-a", got)
	}
	if got := values["@amux_batch_read_b"]; got != "read-b" {
		t.Fatalf("expected @amux_batch_read_b=%q, got %q", "read-b", got)
	}
	if got := values["@amux_batch_read_missing"]; got != "" {
		t.Fatalf("expected missing option to read as empty string, got %q", got)
	}
}
