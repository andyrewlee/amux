package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdDoctorTmuxPruneRequiresYes(t *testing.T) {
	var w bytes.Buffer
	var wErr bytes.Buffer

	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, []string{"--prune"}, "test-v1", nil)
	if code != ExitUsage {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitUsage)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdDoctorTmuxNilQuerySessionRowsFallsBackToDefault(t *testing.T) {
	root := t.TempDir()
	svc := &Services{
		Store:    data.NewWorkspaceStore(root),
		TmuxOpts: tmux.Options{},
		// Intentionally nil: cmdDoctorTmuxWith should fall back to defaultQuerySessionRows.
	}

	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
}

func TestCmdDoctorTmuxJSONSummary(t *testing.T) {
	origLimit := doctorReadSysctlInt
	origInUse := doctorReadPTMXInUse
	doctorReadSysctlInt = func(_ string) (int, bool) { return 100, true }
	doctorReadPTMXInUse = func() (int, bool) { return 20, true }
	defer func() {
		doctorReadSysctlInt = origLimit
		doctorReadPTMXInUse = origInUse
	}()

	rows := []sessionRow{
		{
			name:      "amux-ws-a-term-tab-1",
			tags:      map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"},
			attached:  false,
			createdAt: 100,
		},
		{
			name:      "amux-ws-a-tab-1",
			tags:      map[string]string{"@amux_workspace": "ws-a", "@amux_type": "agent"},
			attached:  true,
			createdAt: 100,
		},
		{name: "other-session", tags: map[string]string{}, attached: false, createdAt: 100},
	}
	svc := testDoctorTmuxServices(t, rows, "ws-a")

	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}

	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	sessions, ok := dataMap["sessions"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessions object, got %T", dataMap["sessions"])
	}
	if got := intFromJSONNumber(t, sessions["total"]); got != 3 {
		t.Fatalf("sessions.total = %d, want 3", got)
	}
	if got := intFromJSONNumber(t, sessions["attached"]); got != 1 {
		t.Fatalf("sessions.attached = %d, want 1", got)
	}
	if got := intFromJSONNumber(t, sessions["detached"]); got != 2 {
		t.Fatalf("sessions.detached = %d, want 2", got)
	}

	candidates, ok := dataMap["candidates"].([]any)
	if !ok {
		t.Fatalf("expected candidates array, got %T", dataMap["candidates"])
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}

	suggestions, ok := dataMap["suggestions"].([]any)
	if !ok {
		t.Fatalf("expected suggestions array, got %T", dataMap["suggestions"])
	}
	if len(suggestions) == 0 {
		t.Fatalf("expected at least one suggestion")
	}
}

func TestCmdDoctorTmuxPruneRuns(t *testing.T) {
	origLimit := doctorReadSysctlInt
	origInUse := doctorReadPTMXInUse
	doctorReadSysctlInt = func(_ string) (int, bool) { return 100, true }
	doctorReadPTMXInUse = func() (int, bool) { return 10, true }
	defer func() {
		doctorReadSysctlInt = origLimit
		doctorReadPTMXInUse = origInUse
	}()

	origKill := tmuxKillSession
	killed := 0
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killed++
		return nil
	}
	defer func() { tmuxKillSession = origKill }()

	rows := []sessionRow{
		{
			name:      "amux-ws-a-term-tab-1",
			tags:      map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"},
			attached:  false,
			createdAt: 100,
		},
		{
			name:      "amux-ws-b-term-tab-1",
			tags:      map[string]string{"@amux_workspace": "ws-b", "@amux_type": "term-tab"},
			attached:  false,
			createdAt: 100,
		},
	}
	svc := testDoctorTmuxServices(t, rows, "ws-a", "ws-b")

	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, []string{"--prune", "--yes"}, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitOK)
	}
	if killed != 2 {
		t.Fatalf("tmuxKillSession calls = %d, want 2", killed)
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	pruneObj, ok := dataMap["prune"].(map[string]any)
	if !ok {
		t.Fatalf("expected prune object, got %T", dataMap["prune"])
	}
	if got := intFromJSONNumber(t, pruneObj["pruned"]); got != 2 {
		t.Fatalf("prune.pruned = %d, want 2", got)
	}
}

func TestCmdDoctorTmuxSessionQueryErrorIsWarn(t *testing.T) {
	origLimit := doctorReadSysctlInt
	origInUse := doctorReadPTMXInUse
	doctorReadSysctlInt = func(_ string) (int, bool) { return 100, true }
	doctorReadPTMXInUse = func() (int, bool) { return 10, true }
	defer func() {
		doctorReadSysctlInt = origLimit
		doctorReadPTMXInUse = origInUse
	}()

	root := t.TempDir()
	svc := &Services{
		Store:    data.NewWorkspaceStore(root),
		TmuxOpts: tmux.Options{},
		QuerySessionRows: func(_ tmux.Options) ([]sessionRow, error) {
			return nil, errors.New("tmux timeout")
		},
	}

	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	checks, ok := dataMap["checks"].([]any)
	if !ok {
		t.Fatalf("expected checks array, got %T", dataMap["checks"])
	}
	foundWarn := false
	for _, item := range checks {
		check, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if check["name"] == "tmux_server" && check["status"] == "warn" {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Fatalf("expected tmux_server warn check, got %#v", checks)
	}
}

func TestCmdDoctorTmuxPruneSessionQueryErrorFails(t *testing.T) {
	root := t.TempDir()
	svc := &Services{
		Store:    data.NewWorkspaceStore(root),
		TmuxOpts: tmux.Options{},
		QuerySessionRows: func(_ tmux.Options) ([]sessionRow, error) {
			return nil, errors.New("tmux timeout")
		},
	}

	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, []string{"--prune", "--yes"}, "test-v1", svc)
	if code != ExitInternalError {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitInternalError)
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "doctor_tmux_prune_failed" {
		t.Fatalf("expected doctor_tmux_prune_failed, got %#v", env.Error)
	}
}

func TestCmdDoctorTmuxOlderThanFiltersCandidates(t *testing.T) {
	origLimit := doctorReadSysctlInt
	origInUse := doctorReadPTMXInUse
	doctorReadSysctlInt = func(_ string) (int, bool) { return 100, true }
	doctorReadPTMXInUse = func() (int, bool) { return 10, true }
	defer func() {
		doctorReadSysctlInt = origLimit
		doctorReadPTMXInUse = origInUse
	}()

	now := time.Now().Unix()
	rows := []sessionRow{
		{
			name:      "amux-ws-a-term-tab-old",
			tags:      map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"},
			attached:  false,
			createdAt: now - 3600,
		},
		{
			name:      "amux-ws-a-term-tab-new",
			tags:      map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"},
			attached:  false,
			createdAt: now - 30,
		},
	}
	svc := testDoctorTmuxServices(t, rows, "ws-a")

	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctorTmuxWith(&w, &wErr, GlobalFlags{JSON: true}, []string{"--older-than", "10m"}, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("cmdDoctorTmuxWith() code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	if got, ok := dataMap["older_than"].(string); !ok || got != "10m" {
		t.Fatalf("older_than = %#v, want %q", dataMap["older_than"], "10m")
	}
	candidates, ok := dataMap["candidates"].([]any)
	if !ok {
		t.Fatalf("expected candidates array, got %T", dataMap["candidates"])
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("expected candidate object, got %T", candidates[0])
	}
	if got, _ := candidate["session"].(string); got != "amux-ws-a-term-tab-old" {
		t.Fatalf("candidate session = %q, want %q", got, "amux-ws-a-term-tab-old")
	}
}

func testDoctorTmuxServices(t *testing.T, rows []sessionRow, workspaceIDs ...string) *Services {
	t.Helper()

	root := t.TempDir()
	for _, wsID := range workspaceIDs {
		dir := filepath.Join(root, wsID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s/workspace.json): %v", dir, err)
		}
	}

	return &Services{
		Store:    data.NewWorkspaceStore(root),
		TmuxOpts: tmux.Options{},
		QuerySessionRows: func(_ tmux.Options) ([]sessionRow, error) {
			return rows, nil
		},
	}
}

func intFromJSONNumber(t *testing.T, v any) int {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("expected float64 JSON number, got %T", v)
	}
	return int(f)
}

func TestReadPTMXOpenCountCountsRows(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available in PATH")
	}
	origCmd := doctorExecCommandContext
	origTimeout := doctorPTMXProbeTimeout
	doctorPTMXProbeTimeout = 100 * time.Millisecond
	doctorExecCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf 'COMMAND\\nproc-a\\nproc-b\\n'")
	}
	defer func() {
		doctorExecCommandContext = origCmd
		doctorPTMXProbeTimeout = origTimeout
	}()

	count, ok := readPTMXOpenCount()
	if !ok {
		t.Fatalf("readPTMXOpenCount() ok = false, want true")
	}
	if count != 2 {
		t.Fatalf("readPTMXOpenCount() count = %d, want 2", count)
	}
}

func TestReadPTMXOpenCountTimesOut(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available in PATH")
	}
	origCmd := doctorExecCommandContext
	origTimeout := doctorPTMXProbeTimeout
	doctorPTMXProbeTimeout = 20 * time.Millisecond
	doctorExecCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sleep", "1")
	}
	defer func() {
		doctorExecCommandContext = origCmd
		doctorPTMXProbeTimeout = origTimeout
	}()

	start := time.Now()
	_, ok := readPTMXOpenCount()
	elapsed := time.Since(start)
	if ok {
		t.Fatalf("readPTMXOpenCount() ok = true, want false")
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("readPTMXOpenCount() elapsed = %v, want < 500ms", elapsed)
	}
}
