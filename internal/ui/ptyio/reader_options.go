package ptyio

import (
	"io"

	tea "charm.land/bubbletea/v2"
)

// ReaderNamespace bundles the per-pane naming and queue tuning that differ
// between the center and sidebar terminal stacks when they start a PTY reader.
// LabelPrefix ("center"/"sidebar") names the reader goroutines and read-loop
// perf label; ReadQueueSize and MaxPendingBytes are the pane's read-queue depth
// and in-flight backpressure ceiling. The shared read-buffer size and frame
// interval come from the ptyio tuning constants, so they are not part of ns.
type ReaderNamespace struct {
	LabelPrefix     string
	ReadQueueSize   int
	MaxPendingBytes int
}

// StartReaderOptionsFor builds the StartReaderOptions both panes pass to
// StartReader, filling in the shared read-buffer size and frame interval and
// the "<prefix>.pty_read_loop/reader/forward" goroutine labels from ns. The
// pane-specific terminal accessor, message factory, and forwarding sink are
// supplied by the caller.
func StartReaderOptionsFor(ns ReaderNamespace, acquire func() io.Reader, factory PTYMsgFactory, forward func(<-chan tea.Msg)) StartReaderOptions {
	return StartReaderOptions{
		AcquireTerm: acquire,
		Config: PTYReaderConfig{
			Label:           ns.LabelPrefix + ".pty_read_loop",
			ReadBufferSize:  PtyReadBufferSize,
			ReadQueueSize:   ns.ReadQueueSize,
			FrameInterval:   PtyFrameInterval,
			MaxPendingBytes: ns.MaxPendingBytes,
		},
		Factory:      factory,
		ReaderLabel:  ns.LabelPrefix + ".pty_reader",
		ForwardLabel: ns.LabelPrefix + ".pty_forward",
		Forward:      forward,
	}
}
