package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) fetchPRComments(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found")
	}
	meta, _ := attempt.Load(wt.Root)
	if meta == nil || meta.PRURL == "" {
		return a.toast.ShowError("No PR linked")
	}
	prURL := meta.PRURL
	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "view", prURL, "--json", "comments,reviews,reviewRequests")
		cmd.Dir = wt.Root
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return messages.PRCommentsLoaded{IssueID: issueID, Err: fmt.Errorf("gh pr view failed: %s", out.String())}
		}
		var payload struct {
			Comments []struct {
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				Body string `json:"body"`
			} `json:"comments"`
			Reviews []struct {
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				Body     string `json:"body"`
				Comments []struct {
					Author struct {
						Login string `json:"login"`
					} `json:"author"`
					Body string `json:"body"`
					Path string `json:"path"`
					Line int    `json:"line"`
				} `json:"comments"`
			} `json:"reviews"`
			ReviewRequests []struct {
				RequestedReviewer struct {
					Login string `json:"login"`
				} `json:"requestedReviewer"`
			} `json:"reviewRequests"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			return messages.PRCommentsLoaded{IssueID: issueID, Err: err}
		}
		options := []string{}
		for _, comment := range payload.Comments {
			options = append(options, fmt.Sprintf("%s: %s", comment.Author.Login, trimComment(comment.Body)))
		}
		for _, review := range payload.Reviews {
			if strings.TrimSpace(review.Body) != "" {
				options = append(options, fmt.Sprintf("review %s: %s", review.Author.Login, trimComment(review.Body)))
			}
			for _, comment := range review.Comments {
				label := comment.Author.Login
				if comment.Path != "" && comment.Line > 0 {
					label = fmt.Sprintf("%s %s:%d", label, comment.Path, comment.Line)
				}
				options = append(options, fmt.Sprintf("%s: %s", label, trimComment(comment.Body)))
			}
		}
		for _, request := range payload.ReviewRequests {
			if request.RequestedReviewer.Login != "" {
				options = append(options, fmt.Sprintf("review request: %s", request.RequestedReviewer.Login))
			}
		}
		return messages.PRCommentsLoaded{IssueID: issueID, Options: options}
	}
}

func trimComment(body string) string {
	body = strings.TrimSpace(body)
	body = strings.ReplaceAll(body, "\n", " ")
	if len(body) > 120 {
		return body[:117] + "..."
	}
	return body
}
