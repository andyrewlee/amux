package app

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/update"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// fakeUpdateService implements UpdateService so checkForUpdates can be exercised
// without reaching the network. result/err are returned verbatim from Check so a
// test can pin both the success and failure translation into UpdateCheckComplete.
type fakeUpdateService struct {
	result *update.CheckResult
	err    error
	calls  int
}

func (s *fakeUpdateService) Check() (*update.CheckResult, error) {
	s.calls++
	return s.result, s.err
}

func (s *fakeUpdateService) Upgrade(*update.Release) error { return nil }
func (s *fakeUpdateService) IsHomebrewBuild() bool         { return false }

// fakeTmuxAvailability implements just enough of TmuxOps to drive
// checkTmuxAvailable: EnsureAvailable controls the available/unavailable branch
// and InstallHint is surfaced on the failure path. Every other method is a
// zero-value stub because checkTmuxAvailable never calls them.
type fakeTmuxAvailability struct {
	ensureErr   error
	installHint string
}

func (f *fakeTmuxAvailability) EnsureAvailable() error {
	return f.ensureErr
}
func (f *fakeTmuxAvailability) InstallHint() string { return f.installHint }
func (f *fakeTmuxAvailability) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return nil, nil
}

func (f *fakeTmuxAvailability) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	return nil, nil
}

func (f *fakeTmuxAvailability) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	return nil, nil
}

func (f *fakeTmuxAvailability) SessionStateFor(string, tmux.Options) (tmux.SessionState, error) {
	return tmux.SessionState{}, nil
}

func (f *fakeTmuxAvailability) SessionHasClients(string, tmux.Options) (bool, error) {
	return false, nil
}
func (f *fakeTmuxAvailability) SessionCreatedAt(string, tmux.Options) (int64, error) { return 0, nil }
func (f *fakeTmuxAvailability) KillSession(string, tmux.Options) error               { return nil }
func (f *fakeTmuxAvailability) KillSessionsMatchingTags(map[string]string, tmux.Options) (bool, error) {
	return false, nil
}
func (f *fakeTmuxAvailability) KillSessionsWithPrefix(string, tmux.Options) error { return nil }
func (f *fakeTmuxAvailability) KillSessionsWithPrefixMissingTag(string, string, tmux.Options) error {
	return nil
}
func (f *fakeTmuxAvailability) KillWorkspaceSessions(string, tmux.Options) error { return nil }
func (f *fakeTmuxAvailability) SetMonitorActivityOn(tmux.Options) error          { return nil }
func (f *fakeTmuxAvailability) SetStatusOff(tmux.Options) error                  { return nil }
func (f *fakeTmuxAvailability) CapturePaneTail(string, int, tmux.Options) (string, bool) {
	return "", false
}
func (f *fakeTmuxAvailability) ContentHash(string) [16]byte { return [16]byte{} }

// ---------------------------------------------------------------------------
// checkForUpdates
// ---------------------------------------------------------------------------

func TestCheckForUpdates(t *testing.T) {
	checkErr := errors.New("network down")
	tests := []struct {
		name    string
		service UpdateService
		want    messages.UpdateCheckComplete
	}{
		{
			name:    "nil service yields empty completion",
			service: nil,
			want:    messages.UpdateCheckComplete{},
		},
		{
			name:    "check error is surfaced verbatim",
			service: &fakeUpdateService{err: checkErr},
			want:    messages.UpdateCheckComplete{Err: checkErr},
		},
		{
			name: "update available is mapped field by field",
			service: &fakeUpdateService{result: &update.CheckResult{
				CurrentVersion:  "1.0.0",
				LatestVersion:   "1.2.0",
				UpdateAvailable: true,
				ReleaseNotes:    "shiny new things",
			}},
			want: messages.UpdateCheckComplete{
				CurrentVersion:  "1.0.0",
				LatestVersion:   "1.2.0",
				UpdateAvailable: true,
				ReleaseNotes:    "shiny new things",
			},
		},
		{
			name: "up to date reports no update available",
			service: &fakeUpdateService{result: &update.CheckResult{
				CurrentVersion:  "2.0.0",
				LatestVersion:   "2.0.0",
				UpdateAvailable: false,
			}},
			want: messages.UpdateCheckComplete{
				CurrentVersion: "2.0.0",
				LatestVersion:  "2.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{updateService: tt.service}
			cmd := app.checkForUpdates()
			if cmd == nil {
				t.Fatal("checkForUpdates returned nil command")
			}
			msg, ok := cmd().(messages.UpdateCheckComplete)
			if !ok {
				t.Fatalf("checkForUpdates produced %T, want messages.UpdateCheckComplete", cmd())
			}
			if msg != tt.want {
				t.Errorf("UpdateCheckComplete = %+v, want %+v", msg, tt.want)
			}
		})
	}
}

func TestCheckForUpdates_InvokesServiceExactlyOnce(t *testing.T) {
	svc := &fakeUpdateService{result: &update.CheckResult{CurrentVersion: "1.0.0"}}
	app := &App{updateService: svc}

	// Building the command must not run the check yet; only invoking it should.
	cmd := app.checkForUpdates()
	if svc.calls != 0 {
		t.Fatalf("Check() called %d times before command invocation, want 0", svc.calls)
	}
	_ = cmd()
	if svc.calls != 1 {
		t.Fatalf("Check() called %d times, want 1", svc.calls)
	}
}

// ---------------------------------------------------------------------------
// checkTmuxAvailable
// ---------------------------------------------------------------------------

func TestCheckTmuxAvailable(t *testing.T) {
	tests := []struct {
		name    string
		service TmuxOps
		want    tmuxAvailableResult
	}{
		{
			name:    "nil service is unavailable with service hint",
			service: nil,
			want:    tmuxAvailableResult{available: false, installHint: "tmux service unavailable"},
		},
		{
			name:    "ensure success reports available with no hint",
			service: &fakeTmuxAvailability{ensureErr: nil},
			want:    tmuxAvailableResult{available: true},
		},
		{
			name: "ensure failure reports unavailable with install hint",
			service: &fakeTmuxAvailability{
				ensureErr:   errors.New("tmux not found"),
				installHint: "brew install tmux",
			},
			want: tmuxAvailableResult{available: false, installHint: "brew install tmux"},
		},
		{
			name: "ensure failure with empty hint stays empty",
			service: &fakeTmuxAvailability{
				ensureErr:   errors.New("tmux not found"),
				installHint: "",
			},
			want: tmuxAvailableResult{available: false, installHint: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{tmuxService: tt.service}
			cmd := app.checkTmuxAvailable()
			if cmd == nil {
				t.Fatal("checkTmuxAvailable returned nil command")
			}
			msg, ok := cmd().(tmuxAvailableResult)
			if !ok {
				t.Fatalf("checkTmuxAvailable produced %T, want tmuxAvailableResult", cmd())
			}
			if msg != tt.want {
				t.Errorf("tmuxAvailableResult = %+v, want %+v", msg, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// tmuxSyncInterval
// ---------------------------------------------------------------------------

func TestTmuxSyncInterval(t *testing.T) {
	tests := []struct {
		name   string
		envSet bool
		envVal string
		want   time.Duration
	}{
		{name: "unset uses default", envSet: false, want: tmuxSyncDefaultInterval},
		{name: "empty string uses default", envSet: true, envVal: "", want: tmuxSyncDefaultInterval},
		{name: "whitespace only uses default", envSet: true, envVal: "   ", want: tmuxSyncDefaultInterval},
		{name: "valid duration is parsed", envSet: true, envVal: "2s", want: 2 * time.Second},
		{name: "valid duration with surrounding spaces is trimmed", envSet: true, envVal: "  500ms  ", want: 500 * time.Millisecond},
		{name: "unparseable value falls back to default", envSet: true, envVal: "not-a-duration", want: tmuxSyncDefaultInterval},
		{name: "zero duration falls back to default", envSet: true, envVal: "0s", want: tmuxSyncDefaultInterval},
		{name: "negative duration falls back to default", envSet: true, envVal: "-5s", want: tmuxSyncDefaultInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envSet {
				t.Setenv("AMUX_TMUX_SYNC_INTERVAL", tt.envVal)
			} else {
				// t.Setenv guarantees the variable is restored after the test, so
				// unsetting here is hermetic for the "unset" case.
				t.Setenv("AMUX_TMUX_SYNC_INTERVAL", "placeholder")
				if err := os.Unsetenv("AMUX_TMUX_SYNC_INTERVAL"); err != nil {
					t.Fatalf("failed to unset env: %v", err)
				}
			}
			app := &App{}
			if got := app.tmuxSyncInterval(); got != tt.want {
				t.Errorf("tmuxSyncInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Ticker constructors
// ---------------------------------------------------------------------------

func TestStartOrphanGCTicker_ReturnsNonNilCmd(t *testing.T) {
	app := &App{}
	// Asserts only the non-nil command: the fixed-interval tea.Tick has no env
	// override, so invoking it would block on the constant interval.
	if app.startOrphanGCTicker() == nil {
		t.Fatal("startOrphanGCTicker returned nil command")
	}
}

func TestStartPTYWatchdog_ReturnsNonNilCmd(t *testing.T) {
	app := &App{}
	// Asserts only the non-nil command: the fixed-interval tea.Tick has no env
	// override, so invoking it would block on the constant interval.
	if app.startPTYWatchdog() == nil {
		t.Fatal("startPTYWatchdog returned nil command")
	}
}

// TestStartTmuxSyncTicker_AllocatesAndThreadsToken pins the only non-trivial
// behavior of the tmux sync ticker: each call bumps syncToken, and the token the
// scheduler will eventually deliver matches the freshly-allocated value. Setting
// a sub-millisecond interval via the AMUX_TMUX_SYNC_INTERVAL seam lets the tick
// fire immediately so the delivered message can be asserted without a long wait.
func TestStartTmuxSyncTicker_AllocatesAndThreadsToken(t *testing.T) {
	t.Setenv("AMUX_TMUX_SYNC_INTERVAL", "1ms")
	app := &App{}

	cmd := app.startTmuxSyncTicker()
	if cmd == nil {
		t.Fatal("startTmuxSyncTicker returned nil command")
	}
	if app.tmuxActivity.syncToken != 1 {
		t.Fatalf("syncToken = %d after first call, want 1", app.tmuxActivity.syncToken)
	}

	msg, ok := cmd().(messages.TmuxSyncTick)
	if !ok {
		t.Fatalf("tmux sync tick produced %T, want messages.TmuxSyncTick", cmd())
	}
	if msg.Token != 1 {
		t.Errorf("delivered TmuxSyncTick.Token = %d, want 1", msg.Token)
	}

	// A second call must allocate a fresh token and thread it through.
	cmd2 := app.startTmuxSyncTicker()
	if app.tmuxActivity.syncToken != 2 {
		t.Fatalf("syncToken = %d after second call, want 2", app.tmuxActivity.syncToken)
	}
	msg2, ok := cmd2().(messages.TmuxSyncTick)
	if !ok {
		t.Fatalf("second tmux sync tick produced %T, want messages.TmuxSyncTick", cmd2())
	}
	if msg2.Token != 2 {
		t.Errorf("second delivered TmuxSyncTick.Token = %d, want 2", msg2.Token)
	}
}

// ---------------------------------------------------------------------------
// applyTmuxEnvFromConfig
// ---------------------------------------------------------------------------

// tmuxEnvKeys are the four environment variables applyTmuxEnvFromConfig manages.
var tmuxEnvKeys = []string{
	config.WorkspacesRootEnvVar,
	"AMUX_TMUX_SERVER",
	"AMUX_TMUX_CONFIG",
	"AMUX_TMUX_SYNC_INTERVAL",
}

// pinTmuxEnv registers cleanups that restore every managed env var to its
// pre-test value, so mutations made by applyTmuxEnvFromConfig do not leak across
// tests.
func pinTmuxEnv(t *testing.T) {
	t.Helper()
	for _, key := range tmuxEnvKeys {
		prev, had := os.LookupEnv(key)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, prev)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func cfgWithTmux(server, configPath, interval, workspacesRoot string) *config.Config {
	return &config.Config{
		Paths: &config.Paths{WorkspacesRoot: workspacesRoot},
		UI: config.UISettings{
			TmuxServer:       server,
			TmuxConfigPath:   configPath,
			TmuxSyncInterval: interval,
		},
	}
}

func TestApplyTmuxEnvFromConfig_NilConfigIsNoOp(t *testing.T) {
	pinTmuxEnv(t)
	for _, key := range tmuxEnvKeys {
		t.Setenv(key, "sentinel-"+key)
	}
	// A nil config must leave the environment untouched.
	applyTmuxEnvFromConfig(nil)
	for _, key := range tmuxEnvKeys {
		if got := os.Getenv(key); got != "sentinel-"+key {
			t.Errorf("nil config mutated %s to %q", key, got)
		}
	}
}

func TestApplyTmuxEnvFromConfig_OnlySetsNonEmpty(t *testing.T) {
	pinTmuxEnv(t)
	// Pre-seed values that should survive because the config leaves them empty.
	t.Setenv("AMUX_TMUX_CONFIG", "preexisting-config")
	t.Setenv("AMUX_TMUX_SYNC_INTERVAL", "preexisting-interval")
	if err := os.Unsetenv(config.WorkspacesRootEnvVar); err != nil {
		t.Fatalf("unset workspaces root: %v", err)
	}
	if err := os.Unsetenv("AMUX_TMUX_SERVER"); err != nil {
		t.Fatalf("unset tmux server: %v", err)
	}

	cfg := cfgWithTmux("amux-server", "", "", "/tmp/ws-root")
	applyTmuxEnvFromConfig(cfg)

	// Non-empty config fields are applied.
	if got := os.Getenv("AMUX_TMUX_SERVER"); got != "amux-server" {
		t.Errorf("AMUX_TMUX_SERVER = %q, want %q", got, "amux-server")
	}
	if got := os.Getenv(config.WorkspacesRootEnvVar); got != "/tmp/ws-root" {
		t.Errorf("%s = %q, want %q", config.WorkspacesRootEnvVar, got, "/tmp/ws-root")
	}
	// Empty config fields must NOT clobber preexisting values.
	if got := os.Getenv("AMUX_TMUX_CONFIG"); got != "preexisting-config" {
		t.Errorf("AMUX_TMUX_CONFIG = %q, want preexisting value preserved", got)
	}
	if got := os.Getenv("AMUX_TMUX_SYNC_INTERVAL"); got != "preexisting-interval" {
		t.Errorf("AMUX_TMUX_SYNC_INTERVAL = %q, want preexisting value preserved", got)
	}
}

// TestApplyTmuxEnvFromConfig_OverwritesNonEmptyFields verifies that non-empty
// config fields overwrite any preexisting env values.
func TestApplyTmuxEnvFromConfig_OverwritesNonEmptyFields(t *testing.T) {
	pinTmuxEnv(t)
	for _, key := range tmuxEnvKeys {
		t.Setenv(key, "stale-"+key)
	}

	cfg := cfgWithTmux("srv", "/etc/tmux.conf", "9s", "/var/ws")
	applyTmuxEnvFromConfig(cfg)

	want := map[string]string{
		"AMUX_TMUX_SERVER":          "srv",
		"AMUX_TMUX_CONFIG":          "/etc/tmux.conf",
		"AMUX_TMUX_SYNC_INTERVAL":   "9s",
		config.WorkspacesRootEnvVar: "/var/ws",
	}
	for key, exp := range want {
		if got := os.Getenv(key); got != exp {
			t.Errorf("overwrite %s = %q, want %q", key, got, exp)
		}
	}
}

// TestApplyTmuxEnvFromConfig_TrimsWhitespace verifies the trimming contract is
// honored through the helpers: a whitespace-only field is treated as empty
// (the preexisting value is preserved) while a padded value is stored trimmed.
func TestApplyTmuxEnvFromConfig_TrimsWhitespace(t *testing.T) {
	pinTmuxEnv(t)
	t.Setenv("AMUX_TMUX_SERVER", "keepme")
	if err := os.Unsetenv("AMUX_TMUX_CONFIG"); err != nil {
		t.Fatalf("unset tmux config: %v", err)
	}

	cfg := cfgWithTmux("   ", "  /padded/path  ", "", "")
	applyTmuxEnvFromConfig(cfg)

	// Whitespace-only server is treated as empty: the preexisting value survives.
	if got := os.Getenv("AMUX_TMUX_SERVER"); got != "keepme" {
		t.Errorf("AMUX_TMUX_SERVER = %q, want preexisting %q (whitespace treated as empty)", got, "keepme")
	}
	// A padded path is stored with surrounding whitespace trimmed.
	if got := os.Getenv("AMUX_TMUX_CONFIG"); got != "/padded/path" {
		t.Errorf("AMUX_TMUX_CONFIG = %q, want trimmed %q", got, "/padded/path")
	}
}
