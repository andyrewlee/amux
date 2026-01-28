package app

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

var urlRegex = regexp.MustCompile(`https?://[^\s]+`)

func (a *App) runIssueScript(issueID, scriptType string) tea.Cmd {
	return a.runScriptForWorktree(a.findWorktreeByIssue(issueID), scriptType)
}

func (a *App) runScriptForWorktree(wt *data.Workspace, scriptType string) tea.Cmd {
	if wt == nil {
		return a.toast.ShowError("No worktree found")
	}
	ptype := process.ScriptType(scriptType)
	// Load workspace metadata to ensure Scripts/ScriptMode fields are populated
	_, _ = a.workspaces.LoadMetadataFor(wt)
	return func() tea.Msg {
		cmd, stdout, stderr, err := a.scripts.RunScriptWithOutputAndCallback(wt, ptype, func(waitErr error) {
			a.scriptOutputCh <- messages.ScriptOutput{
				WorkspaceID: wt.Root,
				ScriptType:  string(ptype),
				Done:        true,
				Err:         waitErr,
			}
		})
		if err != nil {
			return messages.ScriptOutput{
				WorkspaceID: wt.Root,
				ScriptType:  string(ptype),
				Done:        true,
				Err:         err,
			}
		}
		a.updateDrawer()
		if stdout != nil {
			go a.streamScriptOutput(wt.Root, ptype, stdout)
		}
		if stderr != nil {
			go a.streamScriptOutput(wt.Root, ptype, stderr)
		}
		_ = cmd
		return nil
	}
}

func (a *App) streamScriptOutput(root string, scriptType process.ScriptType, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		select {
		case a.scriptOutputCh <- messages.ScriptOutput{
			WorkspaceID: root,
			ScriptType:  string(scriptType),
			Output:      line,
		}:
		default:
		}
	}
}

func (a *App) handleScriptOutput(msg messages.ScriptOutput) {
	if msg.Output != "" {
		line := strings.TrimSpace(msg.Output)
		if line != "" {
			a.logActivityEntry(common.ActivityEntry{
				Kind:      common.ActivityOutput,
				Summary:   line,
				Status:    common.StatusSuccess,
				ProcessID: scriptProcessID(msg.WorkspaceID, msg.ScriptType),
			})
			if url := detectURL(line); url != "" {
				a.previewView.URL = url
				a.previewView.SetRunning(true)
				a.drawer.SetDevURL(url)
			}
		}
	}
	if msg.Done {
		if msg.ScriptType == string(process.ScriptRun) {
			if a.selectedIssue != nil {
				if wt := a.findWorktreeByIssue(a.selectedIssue.ID); wt != nil && wt.Root == msg.WorkspaceID {
					a.previewView.SetRunning(false)
					if msg.Err != nil {
						a.previewView.URL = ""
						a.drawer.SetDevURL("")
					}
					a.inspector.SetScriptRunning(false)
				}
			}
		}
		scriptID := scriptProcessID(msg.WorkspaceID, msg.ScriptType)
		a.updateScriptActivity(scriptID, msg.Err)
		if msg.Err != nil {
			a.toast.ShowError(fmt.Sprintf("Script failed: %v", msg.Err))
		} else {
			a.logActivityEntry(common.ActivityEntry{
				Kind:      common.ActivityInfo,
				Summary:   "Script finished",
				Status:    common.StatusSuccess,
				ProcessID: scriptID,
			})
		}
		a.updateDrawer()
	}
}

func scriptProcessID(root, scriptType string) string {
	if root == "" {
		return ""
	}
	return fmt.Sprintf("script:%s:%s", root, scriptType)
}

func detectURL(line string) string {
	match := urlRegex.FindString(line)
	if match == "" {
		return ""
	}
	if strings.HasSuffix(match, ".") || strings.HasSuffix(match, ",") {
		match = strings.TrimRight(match, ".,")
	}
	return match
}
