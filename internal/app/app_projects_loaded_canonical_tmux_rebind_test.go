package app

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

type rebindCaptureTmuxOps struct {
	stubTmuxOps
	rows       []tmux.SessionTagValues
	matches    []map[string]string
	keys       [][]string
	hasClients map[string]bool
}

func (o *rebindCaptureTmuxOps) SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	o.matches = append(o.matches, maps.Clone(match))
	o.keys = append(o.keys, append([]string(nil), keys...))
	return o.rows, nil
}

func (o *rebindCaptureTmuxOps) SessionHasClients(sessionName string, opts tmux.Options) (bool, error) {
	if o.hasClients == nil {
		return false, nil
	}
	return o.hasClients[sessionName], nil
}

func TestHandleProjectsLoadedCanonicalRebindRetagsTmuxSessionsToNewWorkspaceID(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)

	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	ops := &rebindCaptureTmuxOps{
		rows: []tmux.SessionTagValues{
			{Name: "amux-old-agent", Tags: map[string]string{"@amux_workspace": string(oldWS.ID()), "@amux_instance": "instance-a"}},
			{Name: "amux-detached-terminal", Tags: map[string]string{"@amux_workspace": string(oldWS.ID()), "@amux_instance": "instance-old"}},
			{Name: "amux-other-attached", Tags: map[string]string{"@amux_workspace": string(oldWS.ID()), "@amux_instance": "instance-b"}},
		},
		hasClients: map[string]bool{"amux-other-attached": true},
	}
	var retagged []struct {
		session string
		key     string
		value   string
	}
	origSetTag := setTmuxSessionTagValue
	setTmuxSessionTagValue = func(sessionName, key, value string, opts tmux.Options) error {
		retagged = append(retagged, struct {
			session string
			key     string
			value   string
		}{session: sessionName, key: key, value: value})
		return nil
	}
	defer func() { setTmuxSessionTagValue = origSetTag }()

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		instanceID:      "instance-a",
		projects:        []data.Project{*oldProject},
		activeWorkspace: &oldProject.Workspaces[0],
		activeProject:   oldProject,
		showWelcome:     false,
		tmuxAvailable:   true,
		tmuxService:     newTmuxService(ops),
	}

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*newProject}})
	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	if len(retagged) != 2 {
		t.Fatalf("retagged sessions = %d, want 2", len(retagged))
	}
	gotSessions := map[string]bool{}
	for _, call := range retagged {
		if call.key != "@amux_workspace" {
			t.Fatalf("retag key = %q, want %q", call.key, "@amux_workspace")
		}
		if call.value != string(newWS.ID()) {
			t.Fatalf("retag value = %q, want %q", call.value, newWS.ID())
		}
		gotSessions[call.session] = true
	}
	if !gotSessions["amux-old-agent"] || !gotSessions["amux-detached-terminal"] {
		t.Fatalf("retagged sessions = %v, want current-instance and detached-old-instance sessions", gotSessions)
	}
	if gotSessions["amux-other-attached"] {
		t.Fatalf("retagged sessions = %v, want attached foreign session left unchanged", gotSessions)
	}
	matchedScopedRetag := false
	for _, match := range ops.matches {
		if match["@amux_workspace"] == string(oldWS.ID()) && match["@amux_instance"] == "" {
			matchedScopedRetag = true
			break
		}
	}
	if !matchedScopedRetag {
		t.Fatalf("tmux match calls = %#v, want workspace-only rebind lookup for workspace %q", ops.matches, oldWS.ID())
	}
	requestedInstanceTag := false
	for _, keys := range ops.keys {
		if len(keys) == 1 && keys[0] == "@amux_instance" {
			requestedInstanceTag = true
			break
		}
	}
	if !requestedInstanceTag {
		t.Fatalf("requested tag keys = %#v, want @amux_instance for rebind ownership checks", ops.keys)
	}
}

func TestHandleTmuxAvailableResultFlushesQueuedWorkspaceRebinds(t *testing.T) {
	ops := &rebindCaptureTmuxOps{
		rows: []tmux.SessionTagValues{
			{Name: "amux-detached-agent", Tags: map[string]string{"@amux_workspace": "ws-old", "@amux_instance": "instance-old"}},
		},
	}
	var retagged []struct {
		session string
		key     string
		value   string
	}
	origSetTag := setTmuxSessionTagValue
	setTmuxSessionTagValue = func(sessionName, key, value string, opts tmux.Options) error {
		retagged = append(retagged, struct {
			session string
			key     string
			value   string
		}{session: sessionName, key: key, value: value})
		return nil
	}
	defer func() { setTmuxSessionTagValue = origSetTag }()

	app := &App{
		instanceID:         "instance-a",
		tmuxService:        newTmuxService(ops),
		pendingTmuxRebinds: map[string]string{"ws-old": "ws-new"},
	}

	cmds := app.handleTmuxAvailableResult(tmuxAvailableResult{available: true})
	if len(cmds) == 0 || cmds[0] == nil {
		t.Fatal("expected queued tmux rebind command before other tmux startup work")
	}
	msg := cmds[0]()
	if _, ok := msg.(workspaceTmuxRebindResultMsg); !ok {
		t.Fatalf("expected workspaceTmuxRebindResultMsg, got %T", msg)
	}
	_, _ = app.update(msg)

	if len(retagged) != 1 {
		t.Fatalf("retagged sessions = %d, want 1", len(retagged))
	}
	if retagged[0].session != "amux-detached-agent" {
		t.Fatalf("retagged session = %q, want %q", retagged[0].session, "amux-detached-agent")
	}
	if retagged[0].key != "@amux_workspace" {
		t.Fatalf("retag key = %q, want %q", retagged[0].key, "@amux_workspace")
	}
	if retagged[0].value != "ws-new" {
		t.Fatalf("retag value = %q, want %q", retagged[0].value, "ws-new")
	}
	if len(ops.matches) != 1 {
		t.Fatalf("tmux match calls = %d, want 1 deferred rebind lookup", len(ops.matches))
	}
	if ops.matches[0]["@amux_workspace"] != "ws-old" {
		t.Fatalf("workspace match = %q, want %q", ops.matches[0]["@amux_workspace"], "ws-old")
	}
	if ops.matches[0]["@amux_instance"] != "" {
		t.Fatalf("instance match = %q, want empty broad lookup for detached recovery", ops.matches[0]["@amux_instance"])
	}
	if len(ops.keys) != 1 || len(ops.keys[0]) != 1 || ops.keys[0][0] != "@amux_instance" {
		t.Fatalf("requested tag keys = %#v, want [[@amux_instance]]", ops.keys)
	}
	if len(app.pendingTmuxRebinds) != 0 {
		t.Fatalf("pending tmux rebinds = %v, want empty after successful result", app.pendingTmuxRebinds)
	}
}

func TestWorkspaceTmuxRebindFailureStaysQueuedUntilRetrySucceeds(t *testing.T) {
	ops := &rebindCaptureTmuxOps{
		rows: []tmux.SessionTagValues{
			{Name: "amux-detached-agent", Tags: map[string]string{"@amux_workspace": "ws-old", "@amux_instance": "instance-old"}},
		},
	}
	retagAttempts := 0
	origSetTag := setTmuxSessionTagValue
	setTmuxSessionTagValue = func(sessionName, key, value string, opts tmux.Options) error {
		retagAttempts++
		if retagAttempts == 1 {
			return errors.New("transient retag failure")
		}
		return nil
	}
	defer func() { setTmuxSessionTagValue = origSetTag }()

	app := &App{
		instanceID:         "instance-a",
		tmuxAvailable:      true,
		tmuxService:        newTmuxService(ops),
		pendingTmuxRebinds: map[string]string{"ws-old": "ws-new"},
	}

	initialCmds := app.drainPendingWorkspaceTmuxRebinds()
	if len(initialCmds) != 1 || initialCmds[0] == nil {
		t.Fatalf("initial rebind cmds = %v, want one queued rebind cmd", initialCmds)
	}
	firstMsg := initialCmds[0]()
	if _, ok := firstMsg.(workspaceTmuxRebindResultMsg); !ok {
		t.Fatalf("expected workspaceTmuxRebindResultMsg, got %T", firstMsg)
	}
	_, _ = app.update(firstMsg)
	if got := app.pendingTmuxRebinds["ws-old"]; got != "ws-new" {
		t.Fatalf("pending tmux rebind after failure = %q, want %q", got, "ws-new")
	}

	app.tmuxActivityToken = 1
	app.tmuxActivityScanInFlight = true
	retryCmds := app.handleTmuxActivityResult(tmuxActivityResult{Token: 1, SkipApply: true})
	if len(retryCmds) == 0 || retryCmds[0] == nil {
		t.Fatal("expected pending tmux rebind to retry on activity result")
	}
	retryMsg := retryCmds[0]()
	if _, ok := retryMsg.(workspaceTmuxRebindResultMsg); !ok {
		t.Fatalf("expected workspaceTmuxRebindResultMsg on retry, got %T", retryMsg)
	}
	_, _ = app.update(retryMsg)
	if len(app.pendingTmuxRebinds) != 0 {
		t.Fatalf("pending tmux rebinds = %v, want empty after retry succeeds", app.pendingTmuxRebinds)
	}
}

func TestWorkspaceTmuxRebindForeignAttachedSessionStaysQueuedUntilDetached(t *testing.T) {
	ops := &rebindCaptureTmuxOps{
		rows: []tmux.SessionTagValues{
			{Name: "amux-detached-agent", Tags: map[string]string{"@amux_workspace": "ws-old", "@amux_instance": "instance-old"}},
			{Name: "amux-foreign-attached", Tags: map[string]string{"@amux_workspace": "ws-old", "@amux_instance": "instance-foreign"}},
		},
		hasClients: map[string]bool{"amux-foreign-attached": true},
	}
	var retagged []string
	origSetTag := setTmuxSessionTagValue
	setTmuxSessionTagValue = func(sessionName, key, value string, opts tmux.Options) error {
		retagged = append(retagged, sessionName)
		for i := range ops.rows {
			if ops.rows[i].Name != sessionName {
				continue
			}
			if ops.rows[i].Tags == nil {
				ops.rows[i].Tags = make(map[string]string)
			}
			ops.rows[i].Tags[key] = value
		}
		return nil
	}
	defer func() { setTmuxSessionTagValue = origSetTag }()

	app := &App{
		instanceID:         "instance-a",
		tmuxAvailable:      true,
		tmuxService:        newTmuxService(ops),
		pendingTmuxRebinds: map[string]string{"ws-old": "ws-new"},
	}

	initialCmds := app.drainPendingWorkspaceTmuxRebinds()
	if len(initialCmds) != 1 || initialCmds[0] == nil {
		t.Fatalf("initial rebind cmds = %v, want one queued rebind cmd", initialCmds)
	}
	firstMsg := initialCmds[0]()
	if _, ok := firstMsg.(workspaceTmuxRebindResultMsg); !ok {
		t.Fatalf("expected workspaceTmuxRebindResultMsg, got %T", firstMsg)
	}
	_, _ = app.update(firstMsg)
	if got := app.pendingTmuxRebinds["ws-old"]; got != "ws-new" {
		t.Fatalf("pending tmux rebind after foreign-attached skip = %q, want %q", got, "ws-new")
	}
	if len(retagged) != 1 || retagged[0] != "amux-detached-agent" {
		t.Fatalf("retagged sessions after first attempt = %v, want only detached session", retagged)
	}

	ops.hasClients["amux-foreign-attached"] = false
	app.tmuxActivityToken = 1
	app.tmuxActivityScanInFlight = true
	retryCmds := app.handleTmuxActivityResult(tmuxActivityResult{Token: 1, SkipApply: true})
	if len(retryCmds) == 0 || retryCmds[0] == nil {
		t.Fatal("expected pending tmux rebind to retry after foreign session detaches")
	}
	retryMsg := retryCmds[0]()
	if _, ok := retryMsg.(workspaceTmuxRebindResultMsg); !ok {
		t.Fatalf("expected workspaceTmuxRebindResultMsg on retry, got %T", retryMsg)
	}
	_, _ = app.update(retryMsg)
	if len(app.pendingTmuxRebinds) != 0 {
		t.Fatalf("pending tmux rebinds = %v, want empty after detached retry succeeds", app.pendingTmuxRebinds)
	}
	if len(retagged) < 2 || retagged[len(retagged)-1] != "amux-foreign-attached" {
		t.Fatalf("retagged sessions after retry = %v, want foreign session retagged on second attempt", retagged)
	}
}
