package process

import (
	"sort"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// fakeRunSessionHost is an in-memory RunSessionHost: sessions carry an
// alive/exitCode state so tests can simulate live, finished, and crashed runs
// without tmux.
type fakeRunSessionHost struct {
	sessions  map[string]*fakeRunSession
	ensured   []string // Ensure/Create call order, for name assertions
	killed    []string
	findCalls []string // workspaceID per Find call
	findErr   error
	ensureErr error
	findExtra []string // names Find returns that sessions lacks (vanished)
	// onCreate runs inside Create before the collision check — a test seam
	// that claims the candidate name first to force the retry path.
	onCreate func(host *fakeRunSessionHost, name string)
}

type fakeRunSession struct {
	alive    bool
	exitCode int
	meta     RunSessionMeta
	cmd      string
	workDir  string
	env      []string
	tail     string
}

func newFakeRunSessionHost() *fakeRunSessionHost {
	return &fakeRunSessionHost{sessions: map[string]*fakeRunSession{}}
}

func (f *fakeRunSessionHost) Ensure(name, workDir, cmd string, env []string, meta RunSessionMeta) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	if _, ok := f.sessions[name]; !ok {
		f.sessions[name] = &fakeRunSession{alive: true, exitCode: -1}
		f.ensured = append(f.ensured, name)
	}
	s := f.sessions[name]
	s.meta = meta
	s.cmd = cmd
	s.workDir = workDir
	s.env = env
	return nil
}

// Create is the fake's create-only allocation: it collides on an existing
// name where Ensure would adopt. onCreate runs first so a test can snipe the
// candidate name and force the caller's retry.
func (f *fakeRunSessionHost) Create(name, workDir, cmd string, env []string, meta RunSessionMeta) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	if f.onCreate != nil {
		f.onCreate(f, name)
	}
	if _, ok := f.sessions[name]; ok {
		return ErrRunSessionNameTaken
	}
	f.sessions[name] = &fakeRunSession{alive: true, exitCode: -1, meta: meta, cmd: cmd, workDir: workDir, env: env}
	f.ensured = append(f.ensured, name)
	return nil
}

func (f *fakeRunSessionHost) Status(name string) (exists, alive bool, exitCode int, err error) {
	s, ok := f.sessions[name]
	if !ok {
		return false, false, -1, nil
	}
	return true, s.alive, s.exitCode, nil
}

func (f *fakeRunSessionHost) Kill(name string) error {
	delete(f.sessions, name)
	f.killed = append(f.killed, name)
	return nil
}

func (f *fakeRunSessionHost) Tail(name string, _ int) string {
	if s, ok := f.sessions[name]; ok {
		return s.tail
	}
	return ""
}

func (f *fakeRunSessionHost) Find(workspaceID string) ([]string, error) {
	refs, err := f.FindDetailed(workspaceID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names, nil
}

func (f *fakeRunSessionHost) FindDetailed(workspaceID string) ([]RunSessionRef, error) {
	f.findCalls = append(f.findCalls, workspaceID)
	if f.findErr != nil {
		return nil, f.findErr
	}
	var refs []RunSessionRef
	for name, s := range f.sessions {
		if s.meta.WorkspaceID == workspaceID {
			refs = append(refs, RunSessionRef{Name: name, CreatedAt: s.meta.CreatedAt})
		}
	}
	// findExtra injects names Find returns but Status/Tail don't know — a
	// session that vanished between the sweep and the status read.
	for _, name := range f.findExtra {
		refs = append(refs, RunSessionRef{Name: name})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// die simulates the session's command finishing with the given exit code:
// the session stays (remain-on-exit) but the pane is dead.
func (f *fakeRunSessionHost) die(name string, exitCode int) {
	if s, ok := f.sessions[name]; ok {
		s.alive = false
		s.exitCode = exitCode
	}
}

func newHostedWorkspace(t *testing.T, mode string) *data.Workspace {
	t.Helper()
	ws := &data.Workspace{
		Name:       "ws",
		Root:       t.TempDir(),
		Repo:       t.TempDir(),
		ScriptMode: mode,
		Scripts: data.ScriptsConfig{
			Run: "make dev",
		},
	}
	if ws.ScriptMode == "" {
		ws.ScriptMode = "nonconcurrent"
	}
	return ws
}
