package ptyio

import (
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestStartReaderOptionsForWiresLabelsAndConfig(t *testing.T) {
	forwarded := false
	acquired := false
	opts := StartReaderOptionsFor(
		ReaderNamespace{LabelPrefix: "center", ReadQueueSize: 64, MaxPendingBytes: 512 * 1024},
		func() io.Reader { acquired = true; return nil },
		PTYMsgFactory{
			Output:  func([]byte) tea.Msg { return nil },
			Stopped: func(error) tea.Msg { return nil },
		},
		func(<-chan tea.Msg) { forwarded = true },
	)

	if opts.Config.Label != "center.pty_read_loop" {
		t.Fatalf("Config.Label = %q, want center.pty_read_loop", opts.Config.Label)
	}
	if opts.ReaderLabel != "center.pty_reader" {
		t.Fatalf("ReaderLabel = %q, want center.pty_reader", opts.ReaderLabel)
	}
	if opts.ForwardLabel != "center.pty_forward" {
		t.Fatalf("ForwardLabel = %q, want center.pty_forward", opts.ForwardLabel)
	}
	if opts.Config.ReadQueueSize != 64 || opts.Config.MaxPendingBytes != 512*1024 {
		t.Fatalf("queue/pending = %d/%d, want 64/%d", opts.Config.ReadQueueSize, opts.Config.MaxPendingBytes, 512*1024)
	}
	// Shared tuning comes from the ptyio constants, not the namespace.
	if opts.Config.ReadBufferSize != PtyReadBufferSize || opts.Config.FrameInterval != PtyFrameInterval {
		t.Fatalf("shared tuning not wired: buf=%d frame=%v", opts.Config.ReadBufferSize, opts.Config.FrameInterval)
	}
	// The caller-supplied closures are wired through, not swallowed.
	opts.AcquireTerm()
	opts.Forward(nil)
	if !acquired || !forwarded {
		t.Fatalf("acquire/forward not wired through: acquired=%v forwarded=%v", acquired, forwarded)
	}
}
