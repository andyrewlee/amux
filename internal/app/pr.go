package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/messages"
)

type prInfo struct {
	URL    string
	State  string
	Number int
}

func (a *App) fetchPRStatus(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return nil
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return nil
	}
	meta, _ := attempt.Load(wt.Root)
	if meta == nil || meta.PRURL == "" {
		return nil
	}
	prURL := meta.PRURL
	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "view", prURL, "--json", "state,url,number")
		cmd.Dir = wt.Root
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return messages.PRStatusLoaded{IssueID: issueID, Err: fmt.Errorf("gh pr view failed: %s", out.String())}
		}
		var payload struct {
			State  string `json:"state"`
			URL    string `json:"url"`
			Number int    `json:"number"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			return messages.PRStatusLoaded{IssueID: issueID, Err: err}
		}

		// Auto-complete on merge if configured.
		if a.githubConfig != nil && a.githubConfig.AutoCompleteOnMerge && strings.EqualFold(payload.State, "MERGED") {
			acct := findAccountForIssue(a.linearService, issue)
			if acct.Name != "" && !strings.EqualFold(issue.State.Type, "completed") {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "completed"); stateID != "" {
					_, _ = a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID)
				}
			}
		}

		return messages.PRStatusLoaded{
			IssueID: issueID,
			URL:     payload.URL,
			State:   payload.State,
			Number:  payload.Number,
		}
	}
}

func (a *App) setPRStatus(issueID string, info prInfo) {
	if issueID == "" {
		return
	}
	a.prStatuses[issueID] = info
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.inspector.SetPR(info.URL, info.State, info.Number)
	}
}

func (a *App) prStatus(issueID string) (prInfo, bool) {
	info, ok := a.prStatuses[issueID]
	return info, ok
}
