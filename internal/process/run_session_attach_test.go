package process

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestRunScriptAttachTarget(t *testing.T) {
	setup := func(t *testing.T) (*ScriptRunner, *fakeRunSessionHost, *data.Workspace) {
		t.Helper()
		runner := NewScriptRunner(6200, 10)
		host := newFakeRunSessionHost()
		runner.SetRunHost(host)
		ws := newHostedWorkspace(t, "concurrent")
		return runner, host, ws
	}
	ensure := func(host *fakeRunSessionHost, ws *data.Workspace, name string, alive bool) {
		if err := host.Ensure(name, ws.Root, "true", nil, RunSessionMeta{WorkspaceID: string(ws.MetadataID())}); err != nil {
			t.Fatalf("Ensure(%q): %v", name, err)
		}
		if !alive {
			host.die(name, 0)
		}
	}

	t.Run("newest alive wins", func(t *testing.T) {
		runner, host, ws := setup(t)
		ensure(host, ws, "ws-run", true)
		ensure(host, ws, "ws-run-2", false)
		ensure(host, ws, "ws-run-3", true)
		name, ok := runner.RunScriptAttachTarget(ws)
		if !ok || name != "ws-run-3" {
			t.Fatalf("RunScriptAttachTarget = %q,%v; want ws-run-3,true", name, ok)
		}
	})

	t.Run("newest alive skips dead tail", func(t *testing.T) {
		runner, host, ws := setup(t)
		ensure(host, ws, "ws-run", true)
		ensure(host, ws, "ws-run-2", false)
		name, ok := runner.RunScriptAttachTarget(ws)
		if !ok || name != "ws-run" {
			t.Fatalf("RunScriptAttachTarget = %q,%v; want ws-run,true", name, ok)
		}
	})

	t.Run("none alive", func(t *testing.T) {
		runner, host, ws := setup(t)
		ensure(host, ws, "ws-run", false)
		if name, ok := runner.RunScriptAttachTarget(ws); ok || name != "" {
			t.Fatalf("RunScriptAttachTarget = %q,%v; want \"\",false", name, ok)
		}
	})

	t.Run("no host", func(t *testing.T) {
		runner := NewScriptRunner(6200, 10)
		ws := newHostedWorkspace(t, "nonconcurrent")
		if _, ok := runner.RunScriptAttachTarget(ws); ok {
			t.Fatal("RunScriptAttachTarget with no host = true, want false")
		}
	})

	t.Run("nil workspace", func(t *testing.T) {
		runner, _, _ := setup(t)
		if _, ok := runner.RunScriptAttachTarget(nil); ok {
			t.Fatal("RunScriptAttachTarget(nil) = true, want false")
		}
	})
}
