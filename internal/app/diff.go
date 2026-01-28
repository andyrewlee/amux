package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/diff"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

type reviewComment struct {
	Body string
	Code string
	Side string
	Line int
}

func (a *App) refreshDiff(ignoreWhitespace bool) tea.Cmd {
	issueID := a.diffIssueID
	if issueID == "" && a.selectedIssue != nil {
		issueID = a.selectedIssue.ID
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for diff")
	}
	args := []string{"diff", "--no-color"}
	if ignoreWhitespace {
		args = append(args, "--ignore-all-space")
	}
	return func() tea.Msg {
		mergeBase := wt.Base
		if mergeBase == "" {
			mergeBase = "HEAD"
		}
		if wt.Branch != "" && wt.Base != "" {
			if mb, err := git.RunGit(wt.Root, "merge-base", wt.Branch, wt.Base); err == nil && mb != "" {
				mergeBase = mb
			}
		}
		diffArgs := append(args, mergeBase)
		out, err := git.RunGit(wt.Root, diffArgs...)
		if err != nil {
			return messages.DiffLoaded{Err: fmt.Errorf("git diff failed: %v", err)}
		}
		files := diff.Parse(out)
		return messages.DiffLoaded{Files: files}
	}
}

func (a *App) addDiffComment(file, side string, line int, body string) {
	key := makeCommentKey(file, side, line)
	comment := reviewComment{
		Body: strings.TrimSpace(body),
		Code: a.diffLineSnippet(file, side, line),
		Side: side,
		Line: line,
	}
	a.diffComments[key] = append(a.diffComments[key], comment)
	if a.diffView != nil {
		a.diffView.SetCommentCounts(a.diffCommentCounts())
		a.diffView.SetComments(a.diffCommentBodies())
	}
	if a.inspector != nil {
		a.inspector.SetReviewPreview(a.buildReviewMessage())
	}
}

func (a *App) diffCommentCounts() map[string]int {
	counts := make(map[string]int)
	for key, comments := range a.diffComments {
		if len(comments) == 0 {
			continue
		}
		file, _, _ := splitCommentKey(key)
		counts[file] += len(comments)
	}
	return counts
}

func (a *App) diffCommentBodies() map[string][]string {
	out := make(map[string][]string)
	for key, comments := range a.diffComments {
		if len(comments) == 0 {
			continue
		}
		for _, comment := range comments {
			if comment.Body != "" {
				out[key] = append(out[key], comment.Body)
			}
		}
	}
	return out
}

func makeCommentKey(file, side string, line int) string {
	if side == "" {
		side = "new"
	}
	return fmt.Sprintf("%s::%s::%d", file, side, line)
}

func splitCommentKey(key string) (string, string, int) {
	if strings.Contains(key, "::") {
		parts := strings.Split(key, "::")
		if len(parts) >= 3 {
			line := 0
			for _, r := range parts[len(parts)-1] {
				if r < '0' || r > '9' {
					break
				}
				line = line*10 + int(r-'0')
			}
			return strings.Join(parts[:len(parts)-2], "::"), parts[len(parts)-2], line
		}
	}
	idx := strings.LastIndex(key, ":")
	if idx == -1 {
		return key, "new", 0
	}
	file := key[:idx]
	line := 0
	for _, r := range key[idx+1:] {
		if r < '0' || r > '9' {
			break
		}
		line = line*10 + int(r-'0')
	}
	return file, "new", line
}

func (a *App) diffLineSnippet(file, side string, line int) string {
	if a.diffView == nil || file == "" || line == 0 {
		return ""
	}
	for _, f := range a.diffView.Files {
		if f.Path != file {
			continue
		}
		for _, l := range f.Lines {
			if side == "old" && l.OldLine == line {
				return strings.TrimSpace(stripDiffPrefix(l.Text))
			}
			if side != "old" && l.NewLine == line {
				return strings.TrimSpace(stripDiffPrefix(l.Text))
			}
		}
	}
	return ""
}

func stripDiffPrefix(text string) string {
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		return text[1:]
	}
	if strings.HasPrefix(text, " ") {
		return text[1:]
	}
	return text
}
