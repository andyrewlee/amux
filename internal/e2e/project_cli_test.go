package e2e

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// The real-binary end of the project-facing CLI surface: `amux project ls`
// (registry names, including the duplicate-name repair) and `amux project
// rename` (registry rename, worktree migration, in-place record rebind) —
// the same-basename collision the TUI hits when two repos share a directory
// name. All against a throwaway HOME so the user's real ~/.amux is untouched.

// projectCLIEnv carries the seeded collision state between the test stages
// (setup, ls repair, rename migration, exit-code contract) so each stage
// stays a readable block instead of one 35-branch function.
type projectCLIEnv struct {
	amux         func(args ...string) (string, int, error)
	amuxStreams  func(args ...string) (stdout, stderr string, code int, err error)
	home         string
	amuxHome     string
	metadata     string
	workspaces   string // the shared checkfu container
	codeCheckfu  string
	otherCheckfu string
	recordKeys   map[string]string // workspace leaf -> store key
}

func TestProjectCLIVerbsRealBinary(t *testing.T) {
	skipIfNoGit(t)
	env := newProjectCLIEnv(t)
	assertProjectLSRepairsDuplicateNames(t, env)
	assertProjectRenameMigratesWorktrees(t, env)
	assertProjectRenameExitCodes(t, env)
	assertProjectRenameReportsMigrationFailure(t, env)
	assertProjectListJSONErrorEnvelope(t, env)
}

func newProjectCLIEnv(t *testing.T) *projectCLIEnv {
	t.Helper()
	bin, cleanupBin, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}
	t.Cleanup(cleanupBin)

	home := t.TempDir()
	// HOME is the whole isolation story: DefaultPaths keys off
	// os.UserHomeDir, so the registry, metadata store, and workspaces root
	// all live under this throwaway home.
	envVars := append(stripTestEnv(os.Environ()), "HOME="+home)
	amux := func(args ...string) (string, int, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = envVars
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		return string(out), code, err
	}
	// Separate-buffer variant for the stages that pin a stream contract:
	// the --json error envelope must land on stderr, never stdout.
	amuxStreams := func(args ...string) (string, string, int, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = envVars
		var outBuf, errBuf strings.Builder
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		return outBuf.String(), errBuf.String(), code, err
	}

	// Force the collision: two repos named checkfu under different parents.
	codeRepo := initRepo(t)
	homeRepo := initRepo(t)
	collision := filepath.Join(home, "collision")
	codeCheckfu := filepath.Join(collision, "Code", "checkfu")
	otherCheckfu := filepath.Join(collision, "other", "checkfu")
	for _, pair := range [][2]string{{codeRepo, codeCheckfu}, {homeRepo, otherCheckfu}} {
		if err := os.MkdirAll(filepath.Dir(pair[1]), 0o755); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}
		if err := os.Rename(pair[0], pair[1]); err != nil {
			t.Fatalf("move repo into collision layout: %v", err)
		}
	}

	// The legacy broken state: two registered projects both named checkfu.
	registryJSON := fmt.Sprintf(`{"version":1,"projects":[`+
		`{"name":"checkfu","path":%q},`+
		`{"name":"checkfu","path":%q}]}`, codeCheckfu, otherCheckfu)
	amuxHome := filepath.Join(home, ".amux")
	if err := os.MkdirAll(amuxHome, 0o700); err != nil {
		t.Fatalf("mkdir amux home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(amuxHome, "projects.json"), []byte(registryJSON), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	// One worktree per repo in the shared container, plus their metadata
	// records — the state a real user's collision has (records keyed by
	// (repo, root), worktrees interleaved in one container).
	workspaces := filepath.Join(amuxHome, "workspaces", "checkfu")
	metadata := filepath.Join(amuxHome, "workspaces-metadata")
	if err := os.MkdirAll(workspaces, 0o700); err != nil {
		t.Fatalf("mkdir workspaces: %v", err)
	}
	store := data.NewWorkspaceStore(metadata)
	recordKeys := make(map[string]string) // workspace leaf -> store key
	for _, pair := range [][2]string{{codeCheckfu, "feature"}, {otherCheckfu, "feature-2"}} {
		wsPath := filepath.Join(workspaces, pair[1])
		runGit(t, pair[0], "worktree", "add", "-b", pair[1], "--", wsPath, "HEAD")
		ws := data.NewWorkspace(pair[1], pair[1], "main", pair[0], wsPath)
		if _, _, err := store.SaveIfAbsent(ws); err != nil {
			t.Fatalf("seed record for %s: %v", pair[1], err)
		}
		recordKeys[pair[1]] = string(ws.ID())
	}

	return &projectCLIEnv{
		amux:         amux,
		amuxStreams:  amuxStreams,
		home:         home,
		amuxHome:     amuxHome,
		metadata:     metadata,
		workspaces:   workspaces,
		codeCheckfu:  codeCheckfu,
		otherCheckfu: otherCheckfu,
		recordKeys:   recordKeys,
	}
}

// assertProjectLSRepairsDuplicateNames: ls reports the repaired names
// (registration order keeps the bare name; later ones get -2). The repair is
// in-memory only — ls never writes.
func assertProjectLSRepairsDuplicateNames(t *testing.T, env *projectCLIEnv) {
	t.Helper()
	lsOut, code, err := env.amux("project", "ls")
	if err != nil || code != 0 {
		t.Fatalf("project ls failed (%d, %v):\n%s", code, err, lsOut)
	}
	for _, want := range []string{"checkfu", "checkfu-2", env.codeCheckfu, env.otherCheckfu} {
		if !strings.Contains(lsOut, want) {
			t.Fatalf("project ls missing %q:\n%s", want, lsOut)
		}
	}
}

// assertProjectRenameMigratesWorktrees: renaming the first checkfu moves its
// worktree (git and disk agree), persists the new name, and rebinds the
// record in place under the SAME store key. The other repo's worktree and
// record stay at the shared container.
func assertProjectRenameMigratesWorktrees(t *testing.T, env *projectCLIEnv) {
	t.Helper()
	out, code, err := env.amux("project", "rename", env.codeCheckfu, "checkfu-old")
	if err != nil || code != 0 {
		t.Fatalf("project rename failed (%d, %v):\n%s", code, err, out)
	}
	if !strings.Contains(out, "moved=1") {
		t.Fatalf("rename must report the moved worktree:\n%s", out)
	}

	// The worktree physically lives in the new container and git knows it.
	movedPath := filepath.Join(env.amuxHome, "workspaces", "checkfu-old", "feature")
	if _, err := os.Stat(filepath.Join(movedPath, ".git")); err != nil {
		t.Fatalf("moved worktree missing: %v", err)
	}
	listOut, gitErr := gitOutput(t, env.codeCheckfu, "worktree", "list", "--porcelain")
	if gitErr != nil || !strings.Contains(listOut, movedPath) {
		t.Fatalf("git worktree list must include the moved path:\n%s", listOut)
	}
	// The untouched repo's worktree stayed in the old container.
	stayedPath := filepath.Join(env.workspaces, "feature-2")
	if _, err := os.Stat(filepath.Join(stayedPath, ".git")); err != nil {
		t.Fatalf("other repo's worktree must stay in place: %v", err)
	}

	// Registry now carries the explicit names.
	raw, err := os.ReadFile(filepath.Join(env.amuxHome, "projects.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if !strings.Contains(string(raw), `"checkfu-old"`) {
		t.Fatalf("registry must persist the renamed name:\n%s", raw)
	}

	// The moved workspace's record followed under the SAME store key — the
	// stable identity sessions, tags, and port reservations key on — with
	// its Root rebound to the new container. The other repo's record is
	// untouched at the shared container.
	codeRecord, readErr := os.ReadFile(filepath.Join(env.metadata, env.recordKeys["feature"], "workspace.json"))
	if readErr != nil {
		t.Fatalf("read moved record: %v", readErr)
	}
	if !strings.Contains(string(codeRecord), movedPath) {
		t.Fatalf("moved record must carry the new root:\n%s", codeRecord)
	}
	otherRecord, readErr := os.ReadFile(filepath.Join(env.metadata, env.recordKeys["feature-2"], "workspace.json"))
	if readErr != nil {
		t.Fatalf("read untouched record: %v", readErr)
	}
	if !strings.Contains(string(otherRecord), stayedPath) {
		t.Fatalf("other repo's record must keep the old root:\n%s", otherRecord)
	}
}

// assertProjectRenameExitCodes: the shared verb contract — 1 refused,
// 3 unregistered path, 2 usage.
func assertProjectRenameExitCodes(t *testing.T, env *projectCLIEnv) {
	t.Helper()
	// Renaming to a name another project holds is refused: exit 1.
	out, code, err := env.amux("project", "rename", env.codeCheckfu, "checkfu-2")
	if err == nil || code != 1 {
		t.Fatalf("colliding rename must fail (got code %d):\n%s", code, out)
	}

	// Unregistered path: 3 (not found).
	out, code, err = env.amux("project", "rename", filepath.Join(env.home, "missing"), "whatever")
	if err == nil || code != 3 {
		t.Fatalf("unregistered path must exit 3 (got %d):\n%s", code, out)
	}

	// Usage: 2.
	out, code, err = env.amux("project", "rename", env.codeCheckfu)
	if err == nil || code != 2 {
		t.Fatalf("missing argument must exit 2 (got %d):\n%s", code, out)
	}
}

// assertProjectRenameReportsMigrationFailure: a rename whose migration
// fails before any per-worktree work (the container path is occupied by a
// regular file) carries the error and exits 1 — Failed == 0 with nothing
// moved must not masquerade as success.
func assertProjectRenameReportsMigrationFailure(t *testing.T, env *projectCLIEnv) {
	t.Helper()
	blocker := filepath.Join(env.amuxHome, "workspaces", "blocked-name")
	if err := os.WriteFile(blocker, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	defer os.Remove(blocker)

	out, code, err := env.amux("project", "rename", env.codeCheckfu, "blocked-name")
	if err == nil || code != 1 {
		t.Fatalf("migration failure must exit 1 (got %d):\n%s", code, out)
	}
	if !strings.Contains(out, "migration incomplete") {
		t.Fatalf("failure output must say the migration did not finish:\n%s", out)
	}
}

// assertProjectListJSONErrorEnvelope: the shared verb contract's --json
// failure shape — a machine {"error":{…}} envelope on stderr, never stdout,
// where it would corrupt the JSON-lines output a script parses.
func assertProjectListJSONErrorEnvelope(t *testing.T, env *projectCLIEnv) {
	t.Helper()
	registry := filepath.Join(env.amuxHome, "projects.json")
	if err := os.WriteFile(registry, []byte("not json"), 0o600); err != nil {
		t.Fatalf("corrupt registry: %v", err)
	}
	stdout, stderr, code, err := env.amuxStreams("project", "ls", "--json")
	if err == nil || code != 1 {
		t.Fatalf("corrupt registry must exit 1 (got %d):\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, `{"error":`) {
		t.Fatalf("--json failure must write the machine envelope to stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, `{"error":`) {
		t.Fatalf("the envelope must not leak to stdout:\n%s", stdout)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = stripTestEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return string(out), err
}
