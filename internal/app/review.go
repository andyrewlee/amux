package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) sendReviewFeedback(issueID string) tea.Cmd {
	if len(a.diffComments) == 0 {
		return a.toast.ShowInfo("No review comments")
	}
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	message := a.buildReviewMessage()
	if message == "" {
		return a.toast.ShowInfo("No review comments")
	}
	return func() tea.Msg {
		if wt := a.findWorktreeByIssue(issueID); wt != nil {
			a.center.SendToTerminalForWorktreeID(string(wt.ID()), message+"\n")
		} else {
			a.center.SendToTerminal(message + "\n")
		}
		if !a.issueAuthRequired(issue) {
			acct := findAccountForIssue(a.linearService, issue)
			if acct.Name != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "started"); stateID != "" {
					_, _ = a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID)
				}
			}
		}
		if wt := a.findWorktreeByIssue(issueID); wt != nil {
			if meta, _ := attempt.Load(wt.Root); meta != nil {
				meta.Status = "in_progress"
				meta.LastSyncedAt = time.Now()
				_ = attempt.Save(wt.Root, meta)
			}
		}
		a.diffComments = make(map[string][]reviewComment)
		if a.diffView != nil {
			a.diffView.SetCommentCounts(nil)
			a.diffView.SetComments(nil)
		}
		if a.inspector != nil {
			a.inspector.SetReviewPreview("")
		}
		return messages.LogActivity{Line: "Review feedback sent", Kind: "summary", Status: "success"}
	}
}

func (a *App) buildReviewMessage() string {
	if len(a.diffComments) == 0 {
		return ""
	}
	var b strings.Builder
	type entry struct {
		File string
		Line int
		Body string
		Code string
		Side string
	}
	grouped := map[string][]entry{}
	total := 0
	for key, comments := range a.diffComments {
		if len(comments) == 0 {
			continue
		}
		file, side, line := splitCommentKey(key)
		for _, comment := range comments {
			if strings.TrimSpace(comment.Body) == "" {
				continue
			}
			grouped[file] = append(grouped[file], entry{
				File: file,
				Line: line,
				Body: comment.Body,
				Code: comment.Code,
				Side: side,
			})
			total++
		}
	}
	if total == 0 {
		return ""
	}
	b.WriteString(fmt.Sprintf("## Review Comments (%d)\n\n", total))
	files := make([]string, 0, len(grouped))
	for file := range grouped {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		entries := grouped[file]
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Line == entries[j].Line {
				return entries[i].Body < entries[j].Body
			}
			return entries[i].Line < entries[j].Line
		})
		if file == "" {
			file = "(unknown file)"
		}
		b.WriteString(fmt.Sprintf("- %s\n", file))
		for _, entry := range entries {
			lineLabel := ""
			if entry.Line > 0 {
				sideLabel := ""
				if entry.Side != "" {
					sideLabel = " " + entry.Side
				}
				lineLabel = fmt.Sprintf("L%d%s: ", entry.Line, sideLabel)
			}
			if entry.Code != "" {
				b.WriteString(fmt.Sprintf("  - %s`%s`\n", lineLabel, entry.Code))
			} else {
				b.WriteString(fmt.Sprintf("  - %s\n", strings.TrimSpace(lineLabel)))
			}
			b.WriteString(fmt.Sprintf("    - %s\n", strings.TrimSpace(entry.Body)))
		}
	}
	return b.String()
}
