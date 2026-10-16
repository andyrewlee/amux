// Package ptyio holds the low-level PTY/tmux plumbing shared by the center and
// sidebar terminals: the PTY read loop and output forwarding/merging, PTY-noise
// filtering and overflow trimming, the pane input-writer binding protocol
// (input_binding.go), the attach lifecycle steps and reader lifecycle policy,
// and tmux session bootstrap/restore (capture, snapshot, scrollback). It was
// split out of internal/ui/common so panes that do no terminal I/O don't
// transitively depend on tmux/vterm/safego through it.
//
// The shared pieces are state + policy with explicit parameters and Fn
// seams — the pane owns its lock, its message types, and its lifecycle
// ordering; ptyio owns the mechanics both panes agreed on. The VTerm.Write
// apply side is deliberately NOT here: each pane serializes it differently
// (center's actor-event write path vs sidebar's writer goroutine), which is
// pane-specific policy, not shared mechanics.
package ptyio
