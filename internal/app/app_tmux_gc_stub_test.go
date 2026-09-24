package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
)

// Structural check: the shared fake satisfies TmuxOps without importing app
// (internal/testutil cannot import this package without a cycle).
var _ TmuxOps = (*tmuxops.FakeTmuxOps)(nil)

// This file holds the fake TmuxOps doubles shared by the GC and activity
// tests. Each simulates the *batched* listing contract the fan-out reduction
// relies on: AllSessionMeta/AllSessionStates answer like a real list-sessions/
// list-panes -a — one shot over every session on the instance's socket —
// while the per-session hooks/fixtures supply the answers.

// newStubTmuxOps builds a FakeTmuxOps whose batched listings answer from fixed
// fixtures; every other method is the fake's zero value.
func newStubTmuxOps(allStates map[string]tmux.SessionState, allStatesErr error, allMeta map[string]tmux.SessionMeta) *tmuxops.FakeTmuxOps {
	return &tmuxops.FakeTmuxOps{
		AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
			return allStates, allStatesErr
		},
		AllSessionMetaFunc: func(tmux.Options) (map[string]tmux.SessionMeta, error) {
			return allMeta, nil
		},
	}
}

// gcOrphanOps is a mock TmuxOps for GC safety tests. The shared fake covers
// the boilerplate methods and the kill/tag recorders; the per-session answer
// hooks and the batched-listing synthesis stay local because the simulation
// (instance visibility, knownNames tracking) is this fake's contract.
type gcOrphanOps struct {
	tmuxops.FakeTmuxOps

	sessionsWithTags  func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	sessionHasClients func(name string, opts tmux.Options) (bool, error)
	sessionStateFor   func(name string, opts tmux.Options) (tmux.SessionState, error)
	sessionCreatedAt  func(name string, opts tmux.Options) (int64, error)
	knownNames        []string
	knownInstances    map[string]string
	instanceID        string
}

func (g *gcOrphanOps) SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	if g.sessionsWithTags != nil {
		rows, err := g.sessionsWithTags(match, keys, opts)
		for _, row := range rows {
			g.knownNames = append(g.knownNames, row.Name)
			if g.knownInstances == nil {
				g.knownInstances = make(map[string]string)
			}
			g.knownInstances[row.Name] = strings.TrimSpace(row.Tags["@amux_instance"])
		}
		return rows, err
	}
	return nil, nil
}

// visibleTo reports whether a session would appear in a real list-sessions on
// this instance's socket — foreign state namespaces live on other tmux
// servers, so they never reach the batch output (and are never probed).
func (g *gcOrphanOps) visibleTo(name string) bool {
	return instancesShareState(g.knownInstances[name], g.instanceID)
}

// AllSessionMeta synthesizes the batched listing from the per-session hooks:
// a hook error omits the session entirely, which is how production code sees
// "unverifiable" (missing entry → fail-closed skip).
func (g *gcOrphanOps) AllSessionMeta(opts tmux.Options) (map[string]tmux.SessionMeta, error) {
	out := make(map[string]tmux.SessionMeta, len(g.knownNames))
	for _, name := range g.knownNames {
		if !g.visibleTo(name) {
			continue
		}
		m := tmux.SessionMeta{}
		if has, err := g.SessionHasClients(name, opts); err != nil {
			continue
		} else if has {
			m.Attached = 1
		}
		if ts, err := g.SessionCreatedAt(name, opts); err == nil {
			m.CreatedAt = ts
		}
		out[name] = m
	}
	return out, nil
}

// AllSessionStates synthesizes batched pane state the same way — hook errors
// drop the session from the map.
func (g *gcOrphanOps) AllSessionStates(opts tmux.Options) (map[string]tmux.SessionState, error) {
	out := make(map[string]tmux.SessionState, len(g.knownNames))
	for _, name := range g.knownNames {
		if !g.visibleTo(name) {
			continue
		}
		state, err := g.SessionStateFor(name, opts)
		if err != nil {
			continue
		}
		out[name] = state
	}
	return out, nil
}

func (g *gcOrphanOps) SessionHasClients(name string, opts tmux.Options) (bool, error) {
	if g.sessionHasClients != nil {
		return g.sessionHasClients(name, opts)
	}
	return false, nil
}

func (g *gcOrphanOps) SessionStateFor(name string, opts tmux.Options) (tmux.SessionState, error) {
	if g.sessionStateFor != nil {
		return g.sessionStateFor(name, opts)
	}
	return tmux.SessionState{}, nil
}

func (g *gcOrphanOps) SessionCreatedAt(name string, opts tmux.Options) (int64, error) {
	if g.sessionCreatedAt != nil {
		return g.sessionCreatedAt(name, opts)
	}
	return 0, nil
}

func newGCTestApp(ops *gcOrphanOps) *App {
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		instanceID:     "aaaaaaaaaaaaaaaa.1111111111111111",
		tmuxService:    ops,
	}
	ops.instanceID = app.instanceID
	return app
}

// detachedGCOps serves GC tests from fixture maps. The shared fake covers the
// boilerplate methods and records KillSession calls (KilledSessions); the
// fixture lookups and the SessionsWithTags match recorder stay local.
type detachedGCOps struct {
	tmuxops.FakeTmuxOps

	rows                   []tmux.SessionTagValues
	allStates              map[string]tmux.SessionState
	clients                map[string]bool
	createdAt              map[string]int64
	lastMatch              map[string]string
	sessionHasClientsCalls int
}

func (d *detachedGCOps) SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	d.lastMatch = make(map[string]string, len(match))
	for key, value := range match {
		d.lastMatch[key] = value
	}
	return d.rows, nil
}

func (d *detachedGCOps) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	if d.allStates == nil {
		return map[string]tmux.SessionState{}, nil
	}
	return d.allStates, nil
}

func (d *detachedGCOps) SessionHasClients(sessionName string, opts tmux.Options) (bool, error) {
	d.sessionHasClientsCalls++
	return d.clients[sessionName], nil
}

// AllSessionMeta synthesizes the batched listing from the per-session
// fixtures so tests keep using the clients/createdAt maps.
func (d *detachedGCOps) AllSessionMeta(tmux.Options) (map[string]tmux.SessionMeta, error) {
	out := make(map[string]tmux.SessionMeta)
	for name, hasClients := range d.clients {
		attached := 0
		if hasClients {
			attached = 1
		}
		out[name] = tmux.SessionMeta{Attached: attached}
	}
	for name, ts := range d.createdAt {
		m := out[name]
		m.CreatedAt = ts
		out[name] = m
	}
	return out, nil
}

func (d *detachedGCOps) SessionCreatedAt(sessionName string, opts tmux.Options) (int64, error) {
	return d.createdAt[sessionName], nil
}
