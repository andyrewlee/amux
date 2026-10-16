package sidebar

// Shared sidebar terminal spawn sequence, extracted from the create and
// attach paths (plan 113) — same disease as center's pre-108 twin
// pipelines: every lifecycle fix used to land in one copy and diverge.
//
// The sequence all spawn paths share:
//
//  1. Assemble the pane environment (workspace env + COLORTERM + PATH).
//  2. Run the caller's precheck (create's reuse/squatter check, attach's
//     must-exist target check) — it also decides whether the target is a
//     live session worth bootstrap-capturing.
//  3. Bootstrap-capture the live session when attaching to one.
//  4. Prepare the tmux client command (tags + detach policy).
//  5. Open the PTY at the attach viewport's winsize; on failure abort the
//     prepared command and roll back the bootstrap capture if one ran.
//  6. Finalize attach scrollback for live-session attaches; take a
//     bounded history capture for fresh-session attaches (restart).
//  7. Verify session tags (warn-only) and hand back the products.
//
// Callers own their message types and their precheck semantics — the
// helper returns a plain result + error and each path wraps them in its
// own create/attach result.

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// sidebarSpawnParams parameterizes the shared spawn sequence. Precheck is
// the only behavioral hook: it runs after env assembly and before the
// bootstrap capture, returning (live-session-exists, failure) — the
// "captureExisting" flag for steps 3/5/6.
type sidebarSpawnParams struct {
	ws             *data.Workspace
	sessionName    string
	loginShell     string
	root           string
	envFn          func(*data.Workspace) ([]string, error)
	tags           tmux.SessionTags
	detachExisting bool
	opts           tmux.Options
	// attachWidth/Height drive the PTY winsize and the default capture dims.
	attachWidth, attachHeight int
	// termWidth/Height drive the bootstrap capture's viewport dims.
	termWidth, termHeight int
	// precheck validates the spawn target and reports whether it is a live
	// session the caller is attaching to (bootstrap capture + rollback +
	// finalize apply) — nil means "no check, fresh session".
	precheck func() (bool, error)
	// restartCapture adds the bounded history capture for a fresh-session
	// attach (the restart path); create leaves it off.
	restartCapture bool
	// verifyLabel names the action in the tag-verification warn log.
	verifyLabel string
}

// sidebarSpawnResult carries the products both spawn paths build their
// result messages from.
type sidebarSpawnResult struct {
	term                 *pty.Terminal
	scrollback           []byte
	postAttachScrollback []byte
	captureFullPane      bool
	snapshot             tmux.PaneSnapshot
	captureCols          int
	captureRows          int
}

// spawnSidebarTerminal runs the shared spawn sequence. The returned error
// is already in the caller's failure-message shape — each path wraps it.
func spawnSidebarTerminal(p sidebarSpawnParams) (*sidebarSpawnResult, error) {
	if err := ensureTmuxAvailableFn(); err != nil {
		return nil, err
	}
	var env []string
	if p.envFn != nil {
		var err error
		env, err = p.envFn(p.ws)
		if err != nil {
			return nil, err
		}
	}
	env = append(env, "COLORTERM=truecolor")
	if path := pty.AugmentedPath(); path != "" {
		env = append(env, "PATH="+path)
	}
	captureExisting := false
	if p.precheck != nil {
		exists, err := p.precheck()
		if err != nil {
			return nil, err
		}
		captureExisting = exists
	}
	var bootstrap ptyio.SessionBootstrapCapture
	if captureExisting {
		bootstrap = ptyio.DefaultBootstrap().CaptureExisting(p.sessionName, p.termWidth, p.termHeight, p.opts)
	}
	prepared, err := tmux.NewClientCommand(p.sessionName, tmux.ClientCommandParams{
		WorkDir:        p.root,
		Command:        p.loginShell,
		Environment:    env,
		Options:        p.opts,
		Tags:           p.tags,
		DetachExisting: p.detachExisting,
	})
	if err != nil {
		return nil, err
	}
	ptyRows, ptyCols, _ := pty.WinsizeFromInts(p.attachHeight, p.attachWidth)
	term, err := newPTYWithSizeFn(prepared.Command, p.root, env, ptyRows, ptyCols)
	if err != nil {
		// Start failed before any pane process could exist: the payload
		// is provably unconsumed, then restore the pre-attach capture.
		prepared.AbortBeforeStart()
		if captureExisting {
			ptyio.DefaultBootstrap().Rollback(p.sessionName, bootstrap, p.opts)
		}
		return nil, err
	}
	res := &sidebarSpawnResult{
		term:        term,
		captureCols: p.attachWidth,
		captureRows: p.attachHeight,
	}
	if captureExisting {
		res.scrollback, res.postAttachScrollback, res.captureFullPane, res.snapshot, res.captureCols, res.captureRows = ptyio.FinalizeAttachScrollback(p.sessionName, bootstrap, p.attachWidth, p.attachHeight, p.opts, capturePaneFn)
	}
	if err := verifyTerminalSessionTagsFn(p.sessionName, p.tags, p.opts); err != nil {
		logging.Warn("sidebar terminal %s: session tag verification failed for %s: %v", p.verifyLabel, p.sessionName, err)
	}
	if p.restartCapture {
		res.captureCols, res.captureRows = ptyio.DefaultBootstrap().HistoryCaptureSize(p.sessionName, p.attachWidth, p.attachHeight, p.opts)
		res.scrollback, _ = capturePaneFn(p.sessionName, p.opts)
	}
	return res, nil
}
