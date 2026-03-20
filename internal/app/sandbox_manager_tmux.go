package app

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

const sandboxTmuxNamespace = "amux-sandbox"

func (m *SandboxManager) newTmuxBackedSandboxAgent(wt *data.Workspace, session *sandboxSession, agentType pty.AgentType, sessionName, remoteCommand string, rows, cols uint16, tags tmux.SessionTags) (*pty.Terminal, error) {
	name := tmuxSessionName(wt, agentType, sessionName)
	m.trackTmuxSession(session, name)
	if term, ok, err := m.newExistingTmuxAttachTerminal(name, rows, cols, tags); ok || err != nil {
		return term, err
	}

	cmd, cleanup, err := m.buildSSHCommand(session.sandbox, remoteCommand)
	if err != nil {
		return nil, err
	}
	commandLine := strings.Join(sandbox.ShellQuoteAll(cmd.Args), " ")
	tmuxCommand := tmux.NewClientCommand(name, tmux.ClientCommandParams{
		WorkDir:        wt.Root,
		Command:        commandLine,
		Options:        m.getTmuxOptions(),
		Tags:           tags,
		DetachExisting: true,
	})
	return m.newTmuxSessionTerminal(tmuxCommand, wt.Root, []string{
		"WORKTREE_ROOT=" + session.workspacePath,
		"WORKTREE_NAME=" + wt.Name,
		"COLORTERM=truecolor",
	}, name, rows, cols, cleanup)
}

func (m *SandboxManager) newTmuxBackedSandboxViewer(wt *data.Workspace, session *sandboxSession, command, sessionName, remoteCommand string, rows, cols uint16, tags tmux.SessionTags) (*pty.Terminal, error) {
	name := tmuxSessionNameForViewer(wt, sessionName)
	m.trackTmuxSession(session, name)
	if term, ok, err := m.newExistingTmuxAttachTerminal(name, rows, cols, tags); ok || err != nil {
		return term, err
	}

	cmd, cleanup, err := m.buildSSHCommand(session.sandbox, remoteCommand)
	if err != nil {
		return nil, err
	}
	commandLine := strings.Join(sandbox.ShellQuoteAll(cmd.Args), " ")
	tmuxCommand := tmux.NewClientCommand(name, tmux.ClientCommandParams{
		WorkDir:        wt.Root,
		Command:        commandLine,
		Options:        m.getTmuxOptions(),
		Tags:           tags,
		DetachExisting: true,
	})
	return m.newTmuxSessionTerminal(tmuxCommand, wt.Root, []string{
		"WORKTREE_ROOT=" + session.workspacePath,
		"WORKTREE_NAME=" + wt.Name,
		"COLORTERM=truecolor",
	}, name, rows, cols, cleanup)
}

func (m *SandboxManager) newExistingTmuxAttachTerminal(sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Terminal, bool, error) {
	opts := m.getTmuxOptions()
	state, err := m.sessionStateFor(sessionName, opts)
	if err != nil {
		return nil, false, err
	}
	if !state.Exists {
		return nil, false, nil
	}
	if !state.HasLivePane {
		if err := m.killTmuxSession(sessionName, opts); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if err := m.refreshExistingTmuxSessionTags(sessionName, tags, opts); err != nil {
		return nil, false, err
	}
	term, err := m.attachTmuxTerminal(sessionName, rows, cols, opts)
	if err != nil {
		return nil, false, err
	}
	return term, true, nil
}

func defaultAttachTmuxTerminal(sessionName string, rows, cols uint16, opts tmux.Options) (*pty.Terminal, error) {
	args := []string{}
	if opts.ServerName != "" {
		args = append(args, "-L", opts.ServerName)
	}
	if opts.ConfigPath != "" {
		args = append(args, "-f", opts.ConfigPath)
	}
	args = append(args, "attach", "-dt", "="+sessionName)

	cmd := exec.Command("tmux", args...)
	term, err := pty.NewWithCmd(cmd, nil)
	if err != nil {
		return nil, err
	}
	if rows > 0 && cols > 0 {
		_ = term.SetSize(rows, cols)
	}
	return term, nil
}

func (m *SandboxManager) refreshExistingTmuxSessionTags(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
	if strings.TrimSpace(sessionName) == "" || m.setSessionTagValues == nil {
		return nil
	}
	values := make([]tmux.OptionValue, 0, 8)
	values = append(values, tmux.OptionValue{Key: "@amux", Value: "1"})
	if tags.WorkspaceID != "" {
		values = append(values, tmux.OptionValue{Key: "@amux_workspace", Value: tags.WorkspaceID})
	}
	if tags.TabID != "" {
		values = append(values, tmux.OptionValue{Key: "@amux_tab", Value: tags.TabID})
	}
	if tags.Type != "" {
		values = append(values, tmux.OptionValue{Key: "@amux_type", Value: tags.Type})
	}
	if tags.Runtime != "" {
		values = append(values, tmux.OptionValue{Key: "@amux_runtime", Value: tags.Runtime})
	}
	if tags.Assistant != "" {
		values = append(values, tmux.OptionValue{Key: "@amux_assistant", Value: tags.Assistant})
	}
	if tags.InstanceID != "" {
		values = append(values, tmux.OptionValue{Key: "@amux_instance", Value: tags.InstanceID})
	}
	if tags.SessionOwner != "" {
		values = append(values, tmux.OptionValue{Key: tmux.TagSessionOwner, Value: tags.SessionOwner})
	}
	if tags.LeaseAtMS > 0 {
		values = append(values, tmux.OptionValue{Key: tmux.TagSessionLeaseAt, Value: strconv.FormatInt(tags.LeaseAtMS, 10)})
	}
	return m.setSessionTagValues(sessionName, values, opts)
}

func (m *SandboxManager) newTmuxSessionTerminal(command, dir string, env []string, sessionName string, rows, cols uint16, launchCleanup func()) (*pty.Terminal, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var cleanupOnce sync.Once
	runCleanup := func() {
		if launchCleanup != nil {
			cleanupOnce.Do(launchCleanup)
		}
	}
	clientExited := make(chan struct{})
	var clientExitOnce sync.Once
	signalClientExit := func() {
		clientExitOnce.Do(func() {
			close(clientExited)
		})
	}
	term, err := pty.NewWithCmd(cmd, signalClientExit)
	if err != nil {
		runCleanup()
		return nil, err
	}
	if launchCleanup != nil {
		go m.cleanupTmuxLaunchToken(sessionName, runCleanup, clientExited)
	}
	if rows > 0 && cols > 0 {
		_ = term.SetSize(rows, cols)
	}
	return term, nil
}

func (m *SandboxManager) cleanupTmuxLaunchToken(sessionName string, cleanup func(), clientExited <-chan struct{}) {
	m.mu.Lock()
	ctx := m.shutdownCtx
	m.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	if strings.TrimSpace(sessionName) == "" {
		cleanup()
		return
	}
	pollInterval := m.launchPollInterval
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	steadyPollInterval := pollInterval * 10
	if steadyPollInterval < 100*time.Millisecond {
		steadyPollInterval = 100 * time.Millisecond
	}
	if steadyPollInterval > 2*time.Second {
		steadyPollInterval = 2 * time.Second
	}
	watchTimeout := m.launchWatchTimeout
	if watchTimeout <= 0 {
		watchTimeout = 5 * time.Second
	}
	deadline := time.Now().Add(watchTimeout)
	connected := false
	for ctx.Err() == nil {
		state, err := m.sessionStateFor(sessionName, m.getTmuxOptions())
		if err == nil {
			switch {
			case state.Exists && state.HasLivePane:
				connected = true
			case connected && (!state.Exists || !state.HasLivePane):
				cleanup()
				return
			case !connected && state.Exists && !state.HasLivePane:
				cleanup()
				return
			}
		} else if connected {
			// State query failed after connection — session is gone.
			cleanup()
			return
		}

		if !connected && clientExited != nil {
			select {
			case <-clientExited:
				state, err := m.sessionStateFor(sessionName, m.getTmuxOptions())
				if err == nil && state.Exists && state.HasLivePane {
					connected = true
					continue
				}
				cleanup()
				return
			default:
			}
		}

		nextPoll := pollInterval
		if connected || time.Now().After(deadline) {
			nextPoll = steadyPollInterval
		}
		select {
		case <-ctx.Done():
			cleanup()
			return
		case <-time.After(nextPoll):
		}
	}
	cleanup()
}

func tmuxSessionName(wt *data.Workspace, agentType pty.AgentType, sessionName string) string {
	if strings.TrimSpace(sessionName) != "" {
		return sessionName
	}
	return tmux.SessionName(sandboxTmuxNamespace, string(wt.ID()), string(agentType))
}

func tmuxSessionNameForViewer(wt *data.Workspace, sessionName string) string {
	if strings.TrimSpace(sessionName) != "" {
		return sessionName
	}
	return tmux.SessionName(sandboxTmuxNamespace, string(wt.ID()), "viewer")
}
