package main

// The agent-facing CLI surface (Phase B): `amux --skill` (one-read docs for
// a program hosted in an amux pane), `amux agent ls` (the live fleet with
// reported state), and `amux send` (cli_send.go). Same headless rules as
// the status verbs: no TTY, no TUI, no logging or config initialization —
// a pane script must be able to run all of these.

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/andyrewlee/amux/internal/tmux"
)

// skillDoc is the one-read manual for a hosted agent: everything a program
// inside an amux pane needs to drive its host and its siblings, in a
// single output. Kept factual and current with the real verbs — it is the
// contract an agent can act on without reading the repo.
//
//go:embed skill.md
var skillFS embed.FS

// HandleAgentCLI dispatches the agent-facing subcommands. It returns
// handled=false for any argv it does not own, so it composes with the
// other pre-TTY dispatchers in main.
func HandleAgentCLI(args []string) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}
	switch args[0] {
	case "--skill":
		return true, runSkillCommand(args[1:])
	case "agent":
		return true, runAgentCommand(args[1:])
	case "send":
		return true, runSendCommand(args[1:])
	default:
		return false, 0
	}
}

// runSkillCommand prints the one-read agent docs. It takes no flags: the
// whole point is one command, one read.
func runSkillCommand(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "amux --skill takes no arguments")
		return 2
	}
	doc, err := skillFS.ReadFile("skill.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux: skill doc missing: %v\n", err)
		return 1
	}
	os.Stdout.Write(doc)
	return 0
}

func runAgentCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: amux agent ls [--json] [--workspace NAME]")
		return 2
	}
	if args[0] != "ls" {
		fmt.Fprintf(os.Stderr, "amux agent: unknown subcommand %q (only 'ls')\n", args[0])
		return 2
	}
	return runAgentListCommand(args[1:])
}

// agentListRow is one hosted session as `amux agent ls` reports it. The
// JSON field names are a seam for scripts (same discipline as the tmux tag
// keys: change them only with docs and release notes).
type agentListRow struct {
	Workspace   string `json:"workspace"`
	WorkspaceID string `json:"workspace_id"`
	Type        string `json:"type"`
	Assistant   string `json:"assistant,omitempty"`
	Session     string `json:"session"`
	State       string `json:"state,omitempty"`
	StateSource string `json:"state_source,omitempty"`
	// paneDead records the consumer-side liveness annotation from the
	// report read: the pane's process has exited, so nothing can read
	// input. Deliberately unexported and off the JSON seam — it is a
	// routing fact for `amux send`, not fleet telemetry.
	paneDead bool
}

// matchesWorkspace is the single workspace-join rule for the agent verbs:
// the user-facing workspace name, with the workspace ID as the fallback
// for tag-less comparisons. `agent ls --workspace` filters and
// `send --workspace` resolution share it so the two verbs can never
// disagree about what a workspace selector matches.
func (r agentListRow) matchesWorkspace(workspace string) bool {
	return r.Workspace == workspace || r.WorkspaceID == workspace
}

// runAgentListCommand implements `amux agent ls`: every amux-hosted session
// on the server (agent tabs and run sessions alike) with the strongest
// state signal available — a Program Status Protocol report first, the
// activity scan's published @amux_agent_state second, unknown last. This
// is read-only telemetry; nothing here mutates session or workspace state.
func runAgentListCommand(args []string) int {
	fs := flag.NewFlagSet("amux agent ls", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print one JSON object per session")
	workspace := fs.String("workspace", "", "only sessions of this workspace name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "amux agent ls: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	opts := tmux.OptionsFromTMUXEnv()
	rows, err := hostedAgentRows(opts)
	if err != nil {
		if tmux.IsNoServerError(err) {
			rows = nil // no server: nothing is hosted, that is a valid answer
		} else {
			fmt.Fprintf(os.Stderr, "amux agent ls: %v\n", err)
			return 1
		}
	}
	if *workspace != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.matchesWorkspace(*workspace) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, row := range rows {
			if err := enc.Encode(row); err != nil {
				fmt.Fprintf(os.Stderr, "amux agent ls: %v\n", err)
				return 1
			}
		}
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WORKSPACE\tTYPE\tSTATE\tSESSION")
	for _, row := range rows {
		state := row.State
		if state == "" {
			state = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.Workspace, row.Type, state, row.Session)
	}
	w.Flush()
	return 0
}

// hostedAgentRows reads every amux-hosted session (tagged @amux_workspace
// or @amux_type) and resolves its state, deterministically ordered.
func hostedAgentRows(opts tmux.Options) ([]agentListRow, error) {
	keys := []string{
		"@amux_workspace", "@amux_workspace_name", "@amux_type",
		"@amux_assistant", tmux.TagAgentState,
	}
	sessions, err := tmux.SessionsWithTags(nil, keys, opts)
	if err != nil {
		return nil, err
	}
	reports, reportsErr := tmux.PaneReports(opts)
	if reportsErr != nil && !tmux.IsNoServerError(reportsErr) {
		return nil, reportsErr
	}
	rows := make([]agentListRow, 0, len(sessions))
	for _, session := range sessions {
		id := session.Tags["@amux_workspace"]
		typ := session.Tags["@amux_type"]
		if id == "" && typ == "" {
			continue // not amux-hosted
		}
		row := agentListRow{
			WorkspaceID: id,
			Type:        typ,
			Assistant:   session.Tags["@amux_assistant"],
			Session:     session.Name,
		}
		row.Workspace = session.Tags["@amux_workspace_name"]
		if row.Workspace == "" {
			row.Workspace = id
		}
		// Strongest signal first: a protocol report (honoring the
		// process-exit lifetime — a dead pane's transient working/blocked
		// report is void), then the scan's published heuristic state.
		if r, ok := reports[session.Name]; ok {
			row.paneDead = r.PaneDead
			if r.SurvivesProcessExit() {
				row.State, row.StateSource = r.State, "reported"
			}
		}
		if row.State == "" {
			if tagged := strings.TrimSpace(session.Tags[tmux.TagAgentState]); tagged != "" {
				row.State, row.StateSource = tagged, "scan"
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Workspace != rows[j].Workspace {
			return rows[i].Workspace < rows[j].Workspace
		}
		return rows[i].Session < rows[j].Session
	})
	return rows, nil
}
