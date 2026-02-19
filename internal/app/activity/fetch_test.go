package activity

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// stubFetcher satisfies SessionFetcher for tests.
type stubFetcher struct {
	rows []tmux.SessionTagValues
	err  error
}

func (f stubFetcher) SessionsWithTags(match map[string]string, keys []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
	if len(match) != 0 {
		return nil, errors.New("expected unfiltered SessionsWithTags call")
	}
	if len(keys) == 0 {
		return nil, errors.New("expected non-empty key list")
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f stubFetcher) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return nil, nil
}

type panicFetcher struct{}

func (*panicFetcher) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	panic("typed-nil fetcher should not be called")
}

func (*panicFetcher) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	panic("typed-nil fetcher should not be called")
}

type activeFetcher struct {
	sessions []tmux.SessionActivity
	err      error
}

func (f activeFetcher) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	return nil, nil
}

func (f activeFetcher) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sessions, nil
}

func TestFetchTaggedSessions_IncludesKnownAndTaggedSessions(t *testing.T) {
	rows := []tmux.SessionTagValues{
		{
			Name: "amux-legacyws-tab-1",
			Tags: map[string]string{
				"@amux": "",
			},
		},
		{
			Name: "known-custom",
			Tags: map[string]string{
				"@amux": "",
			},
		},
		{
			Name: "tagged-session",
			Tags: map[string]string{
				"@amux":              "1",
				"@amux_workspace":    "ws-tagged",
				"@amux_type":         "agent",
				tmux.TagLastOutputAt: "1700000000000",
				tmux.TagLastInputAt:  "1700000000000",
			},
		},
		{
			Name: "other-random",
			Tags: map[string]string{
				"@amux": "",
			},
		},
	}
	svc := stubFetcher{rows: rows}
	infoBySession := map[string]SessionInfo{
		"known-custom": {WorkspaceID: "ws-known", IsChat: true},
	}

	got, err := FetchTaggedSessions(svc, infoBySession, tmux.Options{})
	if err != nil {
		t.Fatalf("FetchTaggedSessions: %v", err)
	}

	byName := make(map[string]TaggedSession, len(got))
	for _, session := range got {
		byName[session.Session.Name] = session
	}

	// Untagged sessions not in infoBySession are excluded (legacy heuristic removed).
	if _, ok := byName["amux-legacyws-tab-1"]; ok {
		t.Fatal("expected untagged session not in infoBySession to be excluded")
	}
	if _, ok := byName["known-custom"]; !ok {
		t.Fatal("expected known session without @amux tag to be included")
	}
	if _, ok := byName["tagged-session"]; !ok {
		t.Fatal("expected tagged session to be included")
	}
	if _, ok := byName["other-random"]; ok {
		t.Fatal("expected unrelated untagged session to be excluded")
	}

	if byName["tagged-session"].Session.Tagged != true {
		t.Fatal("expected tagged session to preserve Tagged=true")
	}
	if byName["known-custom"].Session.Tagged {
		t.Fatal("expected known untagged session to preserve Tagged=false")
	}
	if !byName["tagged-session"].HasLastOutput {
		t.Fatal("expected tagged session with timestamp tag to parse last output time")
	}
	if !byName["tagged-session"].HasLastInput {
		t.Fatal("expected tagged session with input tag to parse last input time")
	}
}

func TestIsChatSession(t *testing.T) {
	// Sessions without type or info should not be classified as chat.
	session := tmux.SessionActivity{Name: "other-app-tab-99", Type: ""}
	if IsChatSession(session, SessionInfo{}, false) {
		t.Fatal("session without type or info should not be classified as chat")
	}

	// Sessions without type but with amux naming should also not match
	// (legacy name heuristic removed).
	session2 := tmux.SessionActivity{Name: "amux-ws1-tab-1", Type: "", Tagged: true}
	if IsChatSession(session2, SessionInfo{}, false) {
		t.Fatal("session without type should not be classified as chat even with amux name pattern")
	}

	// Sessions with explicit type should use type regardless of name.
	session3 := tmux.SessionActivity{Name: "random-name", Type: "agent"}
	if !IsChatSession(session3, SessionInfo{}, false) {
		t.Fatal("session with type=agent should be classified as chat")
	}

	// Known tab metadata should win over stale/mismatched session type tags.
	session4 := tmux.SessionActivity{Name: "amux-ws1-tab-2", Type: "terminal"}
	if !IsChatSession(session4, SessionInfo{IsChat: true}, true) {
		t.Fatal("known chat tab should be classified as chat even with stale type tag")
	}

	// Known tabs whose assistant metadata drifted should still honor tmux agent tags.
	session5 := tmux.SessionActivity{Name: "amux-ws1-tab-3", Type: "agent"}
	if !IsChatSession(session5, SessionInfo{IsChat: false}, true) {
		t.Fatal("known session should still be chat when tmux type is explicitly agent")
	}
}

func TestParseLastOutputAtTag(t *testing.T) {
	sec := int64(1_700_000_000)
	if got, ok := ParseLastOutputAtTag("1700000000"); !ok || got.Unix() != sec {
		t.Fatalf("expected seconds parse to %d, got %v (ok=%v)", sec, got, ok)
	}
	ms := int64(1_700_000_000_000)
	if got, ok := ParseLastOutputAtTag("1700000000000"); !ok || got.UnixMilli() != ms {
		t.Fatalf("expected millis parse to %d, got %v (ok=%v)", ms, got, ok)
	}
	ns := int64(1_700_000_000_000_000_000)
	if got, ok := ParseLastOutputAtTag("1700000000000000000"); !ok || got.UnixNano() != ns {
		t.Fatalf("expected nanos parse to %d, got %v (ok=%v)", ns, got, ok)
	}
	if _, ok := ParseLastOutputAtTag(""); ok {
		t.Fatal("expected empty value to be invalid")
	}
	if _, ok := ParseLastOutputAtTag("0"); ok {
		t.Fatal("expected zero to be invalid")
	}
}

func TestFetchTaggedSessions_TypedNilFetcherReturnsUnavailable(t *testing.T) {
	var svc *panicFetcher

	_, err := FetchTaggedSessions(svc, map[string]SessionInfo{}, tmux.Options{})
	if !errors.Is(err, ErrTmuxUnavailable) {
		t.Fatalf("expected ErrTmuxUnavailable, got %v", err)
	}
}

func TestFetchRecentlyActiveByWindow_TypedNilFetcherReturnsUnavailable(t *testing.T) {
	var svc *panicFetcher

	_, err := FetchRecentlyActiveByWindow(svc, time.Second, tmux.Options{})
	if !errors.Is(err, ErrTmuxUnavailable) {
		t.Fatalf("expected ErrTmuxUnavailable, got %v", err)
	}
}

func TestFetchRecentlyActiveByWindow_BuildsNameMap(t *testing.T) {
	svc := activeFetcher{
		sessions: []tmux.SessionActivity{
			{Name: "sess-a"},
			{Name: "   "},
			{Name: "sess-b"},
		},
	}

	got, err := FetchRecentlyActiveByWindow(svc, 5*time.Second, tmux.Options{})
	if err != nil {
		t.Fatalf("FetchRecentlyActiveByWindow: %v", err)
	}
	if !got["sess-a"] || !got["sess-b"] {
		t.Fatalf("expected active map to include sess-a and sess-b, got %v", got)
	}
	if got[""] {
		t.Fatalf("did not expect empty session key in active map: %v", got)
	}
}

func TestFetchRecentlyActiveByWindow_PropagatesFetcherError(t *testing.T) {
	wantErr := errors.New("activity failed")
	svc := activeFetcher{err: wantErr}

	_, err := FetchRecentlyActiveByWindow(svc, time.Second, tmux.Options{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}
