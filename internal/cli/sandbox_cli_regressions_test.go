package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func setCLICommandDeps(
	t *testing.T,
	provider sandbox.Provider,
	meta *sandbox.SandboxMeta,
) {
	t.Helper()

	prevLoadConfig := loadCLIConfig
	prevResolveProvider := resolveCLIProvider
	prevLoadMeta := loadCLISandboxMeta

	loadCLIConfig = func() (sandbox.Config, error) {
		return sandbox.Config{}, nil
	}
	resolveCLIProvider = func(cfg sandbox.Config, cwd, override string) (sandbox.Provider, string, error) {
		return provider, provider.Name(), nil
	}
	loadCLISandboxMeta = func(cwd, providerName string) (*sandbox.SandboxMeta, error) {
		return meta, nil
	}

	t.Cleanup(func() {
		loadCLIConfig = prevLoadConfig
		resolveCLIProvider = prevResolveProvider
		loadCLISandboxMeta = prevLoadMeta
	})
}

func setCLIStdout(t *testing.T) *bytes.Buffer {
	t.Helper()

	prevStdout := cliStdout
	var output bytes.Buffer
	cliStdout = &output
	t.Cleanup(func() {
		cliStdout = prevStdout
	})
	return &output
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevDir)
	})
}

func TestRunAgentAliasUsesInitCwd(t *testing.T) {
	initCwd := t.TempDir()
	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", initCwd)
	chdirForTest(t, t.TempDir())
	setCLIStdout(t)

	prevRunner := runAgentAliasRunner
	prevLoadConfig := loadCLIConfig
	defer func() {
		runAgentAliasRunner = prevRunner
		loadCLIConfig = prevLoadConfig
	}()

	loadCLIConfig = func() (sandbox.Config, error) {
		return sandbox.Config{}, nil
	}

	var got runAgentParams
	runAgentAliasRunner = func(p runAgentParams) error {
		got = p
		return nil
	}

	if err := runAgentAlias(
		"claude",
		nil,
		nil,
		"auto",
		"",
		false,
		30,
		false,
		false,
		false,
		false,
		false,
		0,
		false,
		false,
		false,
		nil,
	); err != nil {
		t.Fatalf("runAgentAlias() error = %v", err)
	}

	if got.cwd != initCwd {
		t.Fatalf("runAgentAlias() cwd = %q, want %q", got.cwd, initCwd)
	}
}

func TestStatusCommandPropagatesProviderErrors(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	getErr := errors.New("api unavailable")
	provider := &resolveCurrentSandboxTestProvider{getErr: getErr}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildStatusCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--json"})

	err := cmd.Execute()
	if !errors.Is(err, getErr) {
		t.Fatalf("status command error = %v, want %v", err, getErr)
	}
}

func TestStatusCommandUsesCurrentCLIWorkingDir(t *testing.T) {
	initCwd := t.TempDir()
	t.Setenv("INIT_CWD", initCwd)
	t.Setenv("PWD", initCwd)
	chdirForTest(t, t.TempDir())
	setCLIStdout(t)

	provider := &resolveCurrentSandboxTestProvider{}

	prevLoadConfig := loadCLIConfig
	prevResolveProvider := resolveCLIProvider
	prevLoadMeta := loadCLISandboxMeta
	defer func() {
		loadCLIConfig = prevLoadConfig
		resolveCLIProvider = prevResolveProvider
		loadCLISandboxMeta = prevLoadMeta
	}()

	loadCLIConfig = func() (sandbox.Config, error) {
		return sandbox.Config{}, nil
	}
	resolveCLIProvider = func(cfg sandbox.Config, cwd, override string) (sandbox.Provider, string, error) {
		return provider, provider.Name(), nil
	}

	var gotMetaCwd string
	loadCLISandboxMeta = func(cwd, providerName string) (*sandbox.SandboxMeta, error) {
		gotMetaCwd = cwd
		return nil, nil
	}

	cmd := buildStatusCommand()
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command error = %v", err)
	}

	if !sameCLIPath(gotMetaCwd, initCwd) {
		t.Fatalf("status meta cwd = %q, want %q", gotMetaCwd, initCwd)
	}
}

func TestExecCommandPropagatesProviderErrors(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	getErr := errors.New("provider auth expired")
	provider := &resolveCurrentSandboxTestProvider{getErr: getErr}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildExecCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"pwd"})

	err := cmd.Execute()
	if !errors.Is(err, getErr) {
		t.Fatalf("exec command error = %v, want %v", err, getErr)
	}
}

func TestExecCommandUsesPersistedWorktreeID(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetExecResult("echo $HOME", "/home/test\n", 0)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{
		SandboxID:  sb.ID(),
		Agent:      sandbox.AgentClaude,
		WorktreeID: "persisted-worktree",
	}
	setCLICommandDeps(t, provider, meta)

	cmd := buildExecCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"pwd"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("exec command error = %v", err)
	}

	history := sb.GetExecHistory()
	if len(history) == 0 {
		t.Fatal("exec history = empty, want executed command")
	}
	last := history[len(history)-1]
	if !strings.Contains(last, "persisted-worktree") {
		t.Fatalf("exec command = %q, want persisted worktree ID", last)
	}
	if strings.Contains(last, sandbox.ComputeWorktreeID(cwd)) {
		t.Fatalf("exec command = %q, should not use current cwd worktree ID", last)
	}
}

func TestSandboxLogsCommandUsesPersistedWorktreeID(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{
		SandboxID:  sb.ID(),
		Agent:      sandbox.AgentClaude,
		WorktreeID: "persisted-worktree",
	}
	setCLICommandDeps(t, provider, meta)

	cmd := buildSandboxLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sandbox logs command error = %v", err)
	}

	history := sb.GetExecHistory()
	if len(history) == 0 {
		t.Fatal("exec history = empty, want log listing command")
	}
	first := history[0]
	if !strings.Contains(first, "/amux/logs/persisted-worktree") {
		t.Fatalf("log command = %q, want persisted worktree log path", first)
	}
	if strings.Contains(first, sandbox.ComputeWorktreeID(cwd)) {
		t.Fatalf("log command = %q, should not use current cwd worktree ID", first)
	}
}

func TestStatusCommandStartedSuggestsAttachCommands(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: sb.ID(), Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildStatusCommand()
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command error = %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "amux ssh") || !strings.Contains(got, "amux exec <cmd>") {
		t.Fatalf("status output = %q, want attach commands", got)
	}
	if strings.Contains(got, "amux sandbox run") {
		t.Fatalf("status output = %q, should not suggest sandbox run for an existing sandbox", got)
	}
}

func TestStatusCommandStoppedSuggestsAttachStartCommands(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetState(sandbox.StateStopped)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: sb.ID(), Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildStatusCommand()
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command error = %v", err)
	}

	got := output.String()
	want := "Sandbox is stopped. Run `amux ssh` or `amux exec <cmd>` to start it."
	if !strings.Contains(got, want) {
		t.Fatalf("status output = %q, want %q", got, want)
	}
	if strings.Contains(got, "amux sandbox run") {
		t.Fatalf("status output = %q, should not suggest sandbox run for an existing sandbox", got)
	}
}
