package activity

import (
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type leaseTagFetcher struct {
	rows []tmux.SessionTagValues
}

func (f leaseTagFetcher) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	return f.rows, nil
}

func (f leaseTagFetcher) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return nil, nil
}

func TestFetchTaggedSessions_UsesLeaseTagWhenOutputTagMissing(t *testing.T) {
	const leaseMS int64 = 1_700_000_000_000
	svc := leaseTagFetcher{
		rows: []tmux.SessionTagValues{
			{
				Name: "tagged-session",
				Tags: map[string]string{
					"@amux":                "1",
					"@amux_workspace":      "ws-tagged",
					"@amux_type":           "agent",
					tmux.TagSessionLeaseAt: strconv.FormatInt(leaseMS, 10),
				},
			},
		},
	}

	got, err := FetchTaggedSessions(svc, map[string]SessionInfo{}, tmux.Options{})
	if err != nil {
		t.Fatalf("FetchTaggedSessions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one tagged session, got %d", len(got))
	}
	if !got[0].HasLastOutput {
		t.Fatal("expected lease tag fallback to set HasLastOutput=true")
	}
	if got[0].LastOutputAt.UnixMilli() != leaseMS {
		t.Fatalf("expected lease-tag timestamp %d, got %d", leaseMS, got[0].LastOutputAt.UnixMilli())
	}
}
