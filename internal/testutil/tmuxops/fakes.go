package tmuxops

// FakeTmuxOps is the hook-driven structural double for app.TmuxOps. It lives in
// its own package because internal/tmux imports internal/process, so a fake
// that references tmux types cannot sit in internal/testutil (process tests
// import testutil — that would cycle). Nil hooks return zero values; kill/tag
// calls are always recorded and mutex-guarded.

import (
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// FakeTmuxOps structurally satisfies app.TmuxOps (declared in internal/app,
// which cannot be imported here) and records the kill/tag calls the previous
// hand-rolled fakes asserted on.
type FakeTmuxOps struct {
	EnsureAvailableFunc                  func() error
	InstallHintFunc                      func() string
	ActiveAgentSessionsByActivityFunc    func(window time.Duration, opts tmux.Options) ([]tmux.SessionActivity, error)
	SessionsWithTagsFunc                 func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	SetSessionTagValueForSessionsFunc    func(sessionNames []string, key, value string, opts tmux.Options) error
	AllSessionStatesFunc                 func(opts tmux.Options) (map[string]tmux.SessionState, error)
	AllSessionMetaFunc                   func(opts tmux.Options) (map[string]tmux.SessionMeta, error)
	SessionStateForFunc                  func(sessionName string, opts tmux.Options) (tmux.SessionState, error)
	SessionHasClientsFunc                func(sessionName string, opts tmux.Options) (bool, error)
	SessionCreatedAtFunc                 func(sessionName string, opts tmux.Options) (int64, error)
	KillSessionFunc                      func(sessionName string, opts tmux.Options) error
	KillSessionsMatchingTagsFunc         func(tags map[string]string, opts tmux.Options) (bool, error)
	KillSessionsWithPrefixFunc           func(prefix string, opts tmux.Options) error
	KillSessionsWithPrefixMissingTagFunc func(prefix, tag string, opts tmux.Options) error
	KillWorkspaceSessionsFunc            func(wsID string, opts tmux.Options) error
	SetMonitorActivityOnFunc             func(opts tmux.Options) error
	SetStatusOffFunc                     func(opts tmux.Options) error
	CapturePaneTailFunc                  func(sessionName string, lines int, opts tmux.Options) (string, bool)
	CapturePaneTailCheckedFunc           func(sessionName string, lines int, activePaneLive bool, opts tmux.Options) (string, bool)
	ContentHashFunc                      func(content string) [16]byte

	mu                     sync.Mutex
	killedSessions         []string
	killTagMatches         []map[string]string
	killTagOpts            []tmux.Options
	killedPrefixes         []string
	killedMissingTag       []PrefixTag
	killedWorkspaceIDs     []string
	tagValueSets           []TagValueSet
	sessionsWithTagsCalls  int
	sessionHasClientsCalls int
}

// PrefixTag records one KillSessionsWithPrefixMissingTag call.
type PrefixTag struct {
	Prefix string
	Tag    string
}

// TagValueSet records one SetSessionTagValueForSessions call.
type TagValueSet struct {
	SessionNames []string
	Key          string
	Value        string
}

func (f *FakeTmuxOps) EnsureAvailable() error {
	if f.EnsureAvailableFunc != nil {
		return f.EnsureAvailableFunc()
	}
	return nil
}

func (f *FakeTmuxOps) InstallHint() string {
	if f.InstallHintFunc != nil {
		return f.InstallHintFunc()
	}
	return ""
}

func (f *FakeTmuxOps) ActiveAgentSessionsByActivity(window time.Duration, opts tmux.Options) ([]tmux.SessionActivity, error) {
	if f.ActiveAgentSessionsByActivityFunc != nil {
		return f.ActiveAgentSessionsByActivityFunc(window, opts)
	}
	return nil, nil
}

func (f *FakeTmuxOps) SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	f.mu.Lock()
	f.sessionsWithTagsCalls++
	f.mu.Unlock()
	if f.SessionsWithTagsFunc != nil {
		return f.SessionsWithTagsFunc(match, keys, opts)
	}
	return nil, nil
}

func (f *FakeTmuxOps) SetSessionTagValueForSessions(sessionNames []string, key, value string, opts tmux.Options) error {
	f.mu.Lock()
	f.tagValueSets = append(f.tagValueSets, TagValueSet{
		SessionNames: append([]string(nil), sessionNames...),
		Key:          key,
		Value:        value,
	})
	f.mu.Unlock()
	if f.SetSessionTagValueForSessionsFunc != nil {
		return f.SetSessionTagValueForSessionsFunc(sessionNames, key, value, opts)
	}
	return nil
}

func (f *FakeTmuxOps) AllSessionStates(opts tmux.Options) (map[string]tmux.SessionState, error) {
	if f.AllSessionStatesFunc != nil {
		return f.AllSessionStatesFunc(opts)
	}
	return nil, nil
}

func (f *FakeTmuxOps) AllSessionMeta(opts tmux.Options) (map[string]tmux.SessionMeta, error) {
	if f.AllSessionMetaFunc != nil {
		return f.AllSessionMetaFunc(opts)
	}
	return nil, nil
}

func (f *FakeTmuxOps) SessionStateFor(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
	if f.SessionStateForFunc != nil {
		return f.SessionStateForFunc(sessionName, opts)
	}
	return tmux.SessionState{}, nil
}

func (f *FakeTmuxOps) SessionHasClients(sessionName string, opts tmux.Options) (bool, error) {
	f.mu.Lock()
	f.sessionHasClientsCalls++
	f.mu.Unlock()
	if f.SessionHasClientsFunc != nil {
		return f.SessionHasClientsFunc(sessionName, opts)
	}
	return false, nil
}

func (f *FakeTmuxOps) SessionCreatedAt(sessionName string, opts tmux.Options) (int64, error) {
	if f.SessionCreatedAtFunc != nil {
		return f.SessionCreatedAtFunc(sessionName, opts)
	}
	return 0, nil
}

func (f *FakeTmuxOps) KillSession(sessionName string, opts tmux.Options) error {
	f.mu.Lock()
	f.killedSessions = append(f.killedSessions, sessionName)
	f.mu.Unlock()
	if f.KillSessionFunc != nil {
		return f.KillSessionFunc(sessionName, opts)
	}
	return nil
}

func (f *FakeTmuxOps) KillSessionsMatchingTags(tags map[string]string, opts tmux.Options) (bool, error) {
	f.mu.Lock()
	copied := make(map[string]string, len(tags))
	for k, v := range tags {
		copied[k] = v
	}
	f.killTagMatches = append(f.killTagMatches, copied)
	f.killTagOpts = append(f.killTagOpts, opts)
	f.mu.Unlock()
	if f.KillSessionsMatchingTagsFunc != nil {
		return f.KillSessionsMatchingTagsFunc(tags, opts)
	}
	return false, nil
}

func (f *FakeTmuxOps) KillSessionsWithPrefix(prefix string, opts tmux.Options) error {
	f.mu.Lock()
	f.killedPrefixes = append(f.killedPrefixes, prefix)
	f.mu.Unlock()
	if f.KillSessionsWithPrefixFunc != nil {
		return f.KillSessionsWithPrefixFunc(prefix, opts)
	}
	return nil
}

func (f *FakeTmuxOps) KillSessionsWithPrefixMissingTag(prefix, tag string, opts tmux.Options) error {
	f.mu.Lock()
	f.killedMissingTag = append(f.killedMissingTag, PrefixTag{Prefix: prefix, Tag: tag})
	f.mu.Unlock()
	if f.KillSessionsWithPrefixMissingTagFunc != nil {
		return f.KillSessionsWithPrefixMissingTagFunc(prefix, tag, opts)
	}
	return nil
}

func (f *FakeTmuxOps) KillWorkspaceSessions(wsID string, opts tmux.Options) error {
	f.mu.Lock()
	f.killedWorkspaceIDs = append(f.killedWorkspaceIDs, wsID)
	f.mu.Unlock()
	if f.KillWorkspaceSessionsFunc != nil {
		return f.KillWorkspaceSessionsFunc(wsID, opts)
	}
	return nil
}

func (f *FakeTmuxOps) SetMonitorActivityOn(opts tmux.Options) error {
	if f.SetMonitorActivityOnFunc != nil {
		return f.SetMonitorActivityOnFunc(opts)
	}
	return nil
}

func (f *FakeTmuxOps) SetStatusOff(opts tmux.Options) error {
	if f.SetStatusOffFunc != nil {
		return f.SetStatusOffFunc(opts)
	}
	return nil
}

func (f *FakeTmuxOps) CapturePaneTail(sessionName string, lines int, opts tmux.Options) (string, bool) {
	if f.CapturePaneTailFunc != nil {
		return f.CapturePaneTailFunc(sessionName, lines, opts)
	}
	return "", false
}

func (f *FakeTmuxOps) CapturePaneTailChecked(sessionName string, lines int, activePaneLive bool, opts tmux.Options) (string, bool) {
	if f.CapturePaneTailCheckedFunc != nil {
		return f.CapturePaneTailCheckedFunc(sessionName, lines, activePaneLive, opts)
	}
	return "", false
}

func (f *FakeTmuxOps) ContentHash(content string) [16]byte {
	if f.ContentHashFunc != nil {
		return f.ContentHashFunc(content)
	}
	return [16]byte{}
}

// KilledSessions returns every session name passed to KillSession.
func (f *FakeTmuxOps) KilledSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killedSessions...)
}

// KillTagMatches returns a copy of every tag map passed to
// KillSessionsMatchingTags — the maps were copied at call time so later
// caller mutation does not leak in.
func (f *FakeTmuxOps) KillTagMatches() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]string, len(f.killTagMatches))
	for i, m := range f.killTagMatches {
		copied := make(map[string]string, len(m))
		for k, v := range m {
			copied[k] = v
		}
		out[i] = copied
	}
	return out
}

// LastKillTagMatch returns the most recent KillSessionsMatchingTags tag map,
// or nil when the method was never called — mirroring the nil-map field the
// hand-rolled recorders exposed.
func (f *FakeTmuxOps) LastKillTagMatch() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.killTagMatches) == 0 {
		return nil
	}
	last := f.killTagMatches[len(f.killTagMatches)-1]
	out := make(map[string]string, len(last))
	for k, v := range last {
		out[k] = v
	}
	return out
}

// LastKillTagOpts returns the tmux.Options of the most recent
// KillSessionsMatchingTags call, or the zero Options when never called.
func (f *FakeTmuxOps) LastKillTagOpts() tmux.Options {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.killTagOpts) == 0 {
		return tmux.Options{}
	}
	return f.killTagOpts[len(f.killTagOpts)-1]
}

// KilledPrefixes returns every prefix passed to KillSessionsWithPrefix.
func (f *FakeTmuxOps) KilledPrefixes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killedPrefixes...)
}

// KilledMissingTag returns every KillSessionsWithPrefixMissingTag call.
func (f *FakeTmuxOps) KilledMissingTag() []PrefixTag {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PrefixTag(nil), f.killedMissingTag...)
}

// KilledWorkspaceIDs returns every wsID passed to KillWorkspaceSessions.
func (f *FakeTmuxOps) KilledWorkspaceIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killedWorkspaceIDs...)
}

// TagValueSets returns every SetSessionTagValueForSessions call.
func (f *FakeTmuxOps) TagValueSets() []TagValueSet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TagValueSet(nil), f.tagValueSets...)
}

// SessionsWithTagsCalls returns how many times SessionsWithTags ran.
func (f *FakeTmuxOps) SessionsWithTagsCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionsWithTagsCalls
}

// SessionHasClientsCalls returns how many times SessionHasClients ran.
func (f *FakeTmuxOps) SessionHasClientsCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionHasClientsCalls
}
