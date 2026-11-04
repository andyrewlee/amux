// Package agentstatus implements amux's consumer half of OSC 7501, the
// Program Status Protocol (revision 0.4): programs hosted in an amux pane self-report their state
// (idle, working, done, blocked, error) through an escape sequence, and
// amux stores what was reported — it never infers status from screen
// output. The wire format is the published spec: colon-separated
// key=value pairs inside OSC 7501 (`state=blocked:kind=question:app=x:msg=<base64>`),
// terminated by BEL or ST; msg and title are base64 of UTF-8; the state
// vocabulary is closed; ids address a record set with clear semantics and
// prompt/exit lifetimes.
//
// Reports reach amux through one channel: the pane's output stream. The
// pipe-pane tap (internal/tmux) scans the raw stream for the sequence,
// maintains the protocol record set (each report replaces its record,
// clear removes records, working/blocked drop at a new shell prompt or
// process exit), and publishes the folded presentation view as tmux pane
// options. The `amux report` CLI is a spec-exact printf onto the same
// stream, so scripted and manual reports are byte-identical to any other
// conformant program.
//
// The package deliberately stays minimal — stdlib plus the internal FSM
// kit (internal/fsm), which backs both table machines — so the scanner,
// parser, record set, and prompt detector are the pieces a future own-PTY
// hosting path (no tmux) reuses verbatim.
package agentstatus
