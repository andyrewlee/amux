package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type doctorTmuxSessionSummary struct {
	Total    int `json:"total"`
	Attached int `json:"attached"`
	Detached int `json:"detached"`
	Amux     int `json:"amux"`
}

type doctorTmuxPruneSummary struct {
	Attempted int      `json:"attempted"`
	Pruned    int      `json:"pruned"`
	Errors    []string `json:"errors"`
}

type doctorTmuxResult struct {
	Checks      []checkResult            `json:"checks"`
	Sessions    doctorTmuxSessionSummary `json:"sessions"`
	Candidates  []pruneEntry             `json:"candidates"`
	OlderThan   string                   `json:"older_than,omitempty"`
	Prune       *doctorTmuxPruneSummary  `json:"prune,omitempty"`
	Suggestions []string                 `json:"suggestions,omitempty"`
}

var (
	// Test seam: these function pointers are overridden in cmd_doctor_tmux_test
	// to keep PTY-capacity checks deterministic without shelling out.
	doctorReadSysctlInt      = readSysctlInt
	doctorReadPTMXInUse      = readPTMXOpenCount
	doctorExecCommandContext = exec.CommandContext
	doctorPTMXProbeTimeout   = 2 * time.Second
)

func cmdDoctorTmux(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return cmdDoctorTmuxWith(w, wErr, gf, args, version, nil)
}

func cmdDoctorTmuxWith(w, wErr io.Writer, gf GlobalFlags, args []string, version string, svc *Services) int {
	const usage = "Usage: amux doctor tmux [--older-than <dur>] [--prune --yes] [--json]"

	fs := newFlagSet("doctor tmux")
	prune := fs.Bool("prune", false, "prune detached/orphaned amux sessions (all ages unless --older-than is set)")
	yes := fs.Bool("yes", false, "confirm prune (required with --prune)")
	olderThan := fs.String("older-than", "", "only include sessions older than duration (e.g. 6h, 30m)")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}
	if *prune && !*yes {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--yes is required when --prune is set"))
	}
	if *yes && !*prune {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--yes requires --prune"))
	}
	var minAge time.Duration
	if *olderThan != "" {
		d, err := time.ParseDuration(*olderThan)
		if err != nil {
			return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("invalid --older-than: %w", err))
		}
		if d <= 0 {
			return returnUsageError(w, wErr, gf, usage, version, errors.New("--older-than must be > 0"))
		}
		minAge = d
	}

	if svc == nil {
		var err error
		svc, err = NewServices(version)
		if err != nil {
			if gf.JSON {
				ReturnError(w, "init_failed", err.Error(), nil, version)
			} else {
				Errorf(wErr, "failed to initialize: %v", err)
			}
			return ExitInternalError
		}
	}

	querySessionRows := svc.QuerySessionRows
	if querySessionRows == nil {
		// Allow tests/callers to provide a partial Services value.
		querySessionRows = defaultQuerySessionRows
	}
	rows, sessionQueryErr := querySessionRows(svc.TmuxOpts)
	if sessionQueryErr != nil {
		if *prune {
			if gf.JSON {
				ReturnError(w, "doctor_tmux_prune_failed", fmt.Sprintf("failed to query tmux sessions: %v", sessionQueryErr), nil, version)
			} else {
				Errorf(wErr, "failed to query tmux sessions: %v", sessionQueryErr)
			}
			return ExitInternalError
		}
		rows = []sessionRow{}
	}
	wsIDs, err := svc.Store.List()
	if err != nil {
		if gf.JSON {
			ReturnError(w, "doctor_tmux_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to list workspaces: %v", err)
		}
		return ExitInternalError
	}

	now := time.Now()
	candidates := findPruneCandidates(rows, wsIDs, minAge, now)
	if candidates == nil {
		candidates = []pruneEntry{}
	}
	summary := summarizeDoctorTmuxSessions(rows)
	serverCheck := checkDoctorTmuxServer(summary)
	if sessionQueryErr != nil {
		serverCheck = checkResult{
			Name:    "tmux_server",
			Status:  "warn",
			Message: fmt.Sprintf("server not reachable (%v)", sessionQueryErr),
		}
	}
	checks := []checkResult{
		checkTmuxInstalled(),
		checkTmuxVersion(),
		serverCheck,
		checkDoctorTmuxPTY(),
		checkDoctorTmuxCandidates(len(candidates)),
	}

	result := doctorTmuxResult{
		Checks:      checks,
		Sessions:    summary,
		Candidates:  candidates,
		OlderThan:   *olderThan,
		Suggestions: doctorTmuxSuggestions(checks, len(candidates), *prune, *olderThan),
	}

	exitCode := ExitOK
	if *prune {
		pruneSummary := runDoctorTmuxPrune(candidates, svc.TmuxOpts)
		result.Prune = &pruneSummary
		if len(pruneSummary.Errors) > 0 {
			exitCode = ExitInternalError
		}
	}

	if gf.JSON {
		if result.Prune != nil && len(result.Prune.Errors) > 0 {
			ReturnError(
				w,
				"doctor_tmux_prune_partial_failed",
				fmt.Sprintf("pruned %d session(s) but %d failed", result.Prune.Pruned, len(result.Prune.Errors)),
				map[string]any{
					"checks":      result.Checks,
					"sessions":    result.Sessions,
					"candidates":  result.Candidates,
					"prune":       result.Prune,
					"suggestions": result.Suggestions,
				},
				version,
			)
		} else {
			PrintJSON(w, result, version)
		}
		return exitCode
	}

	PrintHuman(w, func(w io.Writer) {
		for _, c := range result.Checks {
			icon := "+"
			if c.Status == "warn" {
				icon = "!"
			} else if c.Status == "fail" {
				icon = "x"
			}
			fmt.Fprintf(w, "  [%s] %-25s %s\n", icon, c.Name, c.Message)
		}
		if len(result.Candidates) > 0 {
			fmt.Fprintf(w, "\nPrune candidates (%d):\n", len(result.Candidates))
			for _, c := range result.Candidates {
				fmt.Fprintf(w, "  %-45s (%s, %s old)\n", c.Session, humanReason(c.Reason), formatAge(c.AgeSeconds))
			}
		}
		if result.Prune != nil {
			fmt.Fprintf(w, "\nPrune result: %d/%d session(s) removed\n", result.Prune.Pruned, result.Prune.Attempted)
			for _, pruneErr := range result.Prune.Errors {
				fmt.Fprintf(w, "  Error: %s\n", pruneErr)
			}
		}
		if len(result.Suggestions) > 0 {
			fmt.Fprintln(w, "\nSuggested next commands:")
			for _, suggestion := range result.Suggestions {
				fmt.Fprintf(w, "  %s\n", suggestion)
			}
		}
	})
	return exitCode
}

func summarizeDoctorTmuxSessions(rows []sessionRow) doctorTmuxSessionSummary {
	summary := doctorTmuxSessionSummary{Total: len(rows)}
	for _, row := range rows {
		if row.attached {
			summary.Attached++
		} else {
			summary.Detached++
		}
		if isAmuxSession(row) {
			summary.Amux++
		}
	}
	return summary
}

func checkDoctorTmuxServer(summary doctorTmuxSessionSummary) checkResult {
	if summary.Total == 0 {
		return checkResult{Name: "tmux_server", Status: "warn", Message: "server reachable, 0 session(s)"}
	}
	return checkResult{
		Name:    "tmux_server",
		Status:  "ok",
		Message: fmt.Sprintf("%d session(s), %d attached, %d detached", summary.Total, summary.Attached, summary.Detached),
	}
}

func checkDoctorTmuxCandidates(count int) checkResult {
	if count == 0 {
		return checkResult{Name: "prune_candidates", Status: "ok", Message: "0 detached/orphaned amux sessions"}
	}
	return checkResult{
		Name:    "prune_candidates",
		Status:  "warn",
		Message: fmt.Sprintf("%d detached/orphaned amux session(s)", count),
	}
}

func checkDoctorTmuxPTY() checkResult {
	limit, inUse, ok := detectPTYCapacity()
	if !ok || limit <= 0 {
		return checkResult{Name: "pty_capacity", Status: "warn", Message: "unable to determine PTY usage"}
	}

	ratio := float64(inUse) / float64(limit)
	status := "ok"
	if inUse >= limit {
		status = "fail"
	} else if ratio >= 0.9 {
		status = "warn"
	}

	return checkResult{
		Name:    "pty_capacity",
		Status:  status,
		Message: fmt.Sprintf("%d/%d PTYs in use (%.0f%%)", inUse, limit, ratio*100),
	}
}

func detectPTYCapacity() (limit, inUse int, ok bool) {
	// macOS/BSD style
	if limitValue, maxOK := doctorReadSysctlInt("kern.tty.ptmx_max"); maxOK && limitValue > 0 {
		if used, usedOK := doctorReadPTMXInUse(); usedOK {
			return limitValue, used, true
		}
		if used, usedOK := doctorReadSysctlInt("kern.tty.ptmx_cnt"); usedOK {
			return limitValue, used, true
		}
	}
	// Linux style
	if limitValue, maxOK := doctorReadSysctlInt("kernel.pty.max"); maxOK && limitValue > 0 {
		if used, usedOK := doctorReadSysctlInt("kernel.pty.nr"); usedOK {
			return limitValue, used, true
		}
	}
	return 0, 0, false
}

func doctorTmuxSuggestions(checks []checkResult, candidateCount int, pruned bool, olderThan string) []string {
	var suggestions []string
	ptyWarn := false
	for _, c := range checks {
		if c.Name == "pty_capacity" && (c.Status == "warn" || c.Status == "fail") {
			ptyWarn = true
			break
		}
	}

	if candidateCount > 0 && !pruned {
		pruneWindow := strings.TrimSpace(olderThan)
		if pruneWindow == "" {
			pruneWindow = "6h"
		}
		suggestions = append(suggestions, "amux doctor tmux --prune --yes --older-than "+pruneWindow)
		suggestions = append(suggestions, "make tmux-prune")
	}
	if ptyWarn {
		suggestions = append(suggestions, "amux session list")
	}
	return suggestions
}

func runDoctorTmuxPrune(candidates []pruneEntry, opts tmux.Options) doctorTmuxPruneSummary {
	out := doctorTmuxPruneSummary{
		Attempted: len(candidates),
		Errors:    []string{},
	}
	for _, candidate := range candidates {
		if err := tmuxKillSession(candidate.Session, opts); err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", candidate.Session, err))
			continue
		}
		out.Pruned++
	}
	return out
}

func readSysctlInt(key string) (int, bool) {
	out, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(out))
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return n, true
}

func readPTMXOpenCount() (int, bool) {
	// Best-effort metric only: lsof can be unavailable/slow or blocked by local
	// policy, and callers already degrade to a non-fatal warning on failure.
	ctx, cancel := context.WithTimeout(context.Background(), doctorPTMXProbeTimeout)
	defer cancel()
	out, err := doctorExecCommandContext(ctx, "lsof", "/dev/ptmx").Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= 1 {
		return 0, true
	}
	return len(lines) - 1, true
}
