package center

import (
	"testing"
)

func TestExtractAmuxMessagesFiltersControlLines(t *testing.T) {
	m := &Model{}
	tab := &Tab{}
	data := []byte("hello\nAMUX_LOG: Something happened\nworld\nAMUX_APPROVAL: abc123: {\"foo\":\"bar\"}\n")
	out, msgs := m.extractAmuxMessages(tab, "wt-1", TabID("tab-1"), data)
	if string(out) != "hello\nworld\n" {
		t.Fatalf("unexpected output: %q", string(out))
	}
	if len(msgs.Logs) != 1 {
		t.Fatalf("expected 1 log message, got %d", len(msgs.Logs))
	}
	if msgs.Logs[0].Message != "Something happened" {
		t.Fatalf("expected log message 'Something happened', got %q", msgs.Logs[0].Message)
	}
	if len(msgs.Approvals) != 1 {
		t.Fatalf("expected 1 approval message, got %d", len(msgs.Approvals))
	}
	if msgs.Approvals[0].ID != "abc123" {
		t.Fatalf("expected approval ID 'abc123', got %q", msgs.Approvals[0].ID)
	}
	if msgs.Approvals[0].JSON != "{\"foo\":\"bar\"}" {
		t.Fatalf("expected approval JSON, got %q", msgs.Approvals[0].JSON)
	}
}

func TestExtractAmuxMessagesBuffersIncomplete(t *testing.T) {
	m := &Model{}
	tab := &Tab{}
	// Incomplete AMUX line should be buffered
	data := []byte("hello\nAMUX_LOG: partial")
	out, msgs := m.extractAmuxMessages(tab, "wt-1", TabID("tab-1"), data)
	if string(out) != "hello\n" {
		t.Fatalf("unexpected output: %q", string(out))
	}
	if len(msgs.Logs) != 0 {
		t.Fatalf("expected no log messages for incomplete line, got %d", len(msgs.Logs))
	}
	if string(tab.amuxBuffer) != "AMUX_LOG: partial" {
		t.Fatalf("expected buffered data, got %q", string(tab.amuxBuffer))
	}

	// Complete the line in next chunk
	data2 := []byte(" message\nmore text\n")
	out2, msgs2 := m.extractAmuxMessages(tab, "wt-1", TabID("tab-1"), data2)
	if string(out2) != "more text\n" {
		t.Fatalf("unexpected output: %q", string(out2))
	}
	if len(msgs2.Logs) != 1 {
		t.Fatalf("expected 1 log message, got %d", len(msgs2.Logs))
	}
	if msgs2.Logs[0].Message != "partial message" {
		t.Fatalf("expected combined message, got %q", msgs2.Logs[0].Message)
	}
}
