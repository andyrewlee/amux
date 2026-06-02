// Package ptyio holds the low-level PTY/tmux plumbing shared by the center and
// sidebar terminals: the PTY read loop and output forwarding/merging, PTY-noise
// filtering and overflow trimming, and tmux session bootstrap/restore (capture,
// snapshot, scrollback). It was split out of internal/ui/common so panes that do
// no terminal I/O don't transitively depend on tmux/vterm/safego through it.
package ptyio
