package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// --- session list ---

type sessionListEntry struct {
	SessionName string `json:"session_name"`
	WorkspaceID string `json:"workspace_id"`
	Type        string `json:"type"`
	Attached    bool   `json:"attached"`
	CreatedAt   int64  `json:"created_at"`
	AgeSeconds  int64  `json:"age_seconds"`
}

func cmdSessionList(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return cmdSessionListWith(w, wErr, gf, args, version, nil)
}

func cmdSessionListWith(w, wErr io.Writer, gf GlobalFlags, args []string, version string, svc *Services) int {
	const usage = "Usage: amux session list [--json]"
	if len(args) > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(args, " ")))
	}

	if svc == nil {
		var code int
		svc, code = initServicesOrFail(w, wErr, gf, version)
		if code >= 0 {
			return code
		}
	}

	rows, err := svc.QuerySessionRows(svc.TmuxOpts)
	if err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "list_failed", err, nil,
			"failed to list sessions: %v", err)
	}

	entries := buildSessionList(rows, time.Now())

	if gf.JSON {
		PrintJSON(w, entries, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		if len(entries) == 0 {
			fmt.Fprintln(w, "No sessions.")
			return
		}
		for _, e := range entries {
			attached := ""
			if e.Attached {
				attached = " (attached)"
			}
			fmt.Fprintf(w, "  %-45s ws=%-16s type=%-12s age=%s%s\n",
				e.SessionName, e.WorkspaceID, e.Type, formatAge(e.AgeSeconds), attached)
		}
	})
	return ExitOK
}

// --- session prune ---

type pruneEntry struct {
	Session     string `json:"session"`
	WorkspaceID string `json:"workspace_id"`
	Reason      string `json:"reason"`
	AgeSeconds  int64  `json:"age_seconds"`
}

type pruneResult struct {
	DryRun bool         `json:"dry_run"`
	Pruned []pruneEntry `json:"pruned"`
	Total  int          `json:"total"`
	Errors []string     `json:"errors"`
}

func cmdSessionPrune(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return cmdSessionPruneWith(w, wErr, gf, args, version, nil)
}

func cmdSessionPruneWith(w, wErr io.Writer, gf GlobalFlags, args []string, version string, svc *Services) int {
	const usage = "Usage: amux session prune [--yes] [--older-than <dur>] [--json]"
	fs := newFlagSet("session prune")
	yes := fs.Bool("yes", false, "confirm prune (required)")
	olderThan := fs.String("older-than", "", "only prune sessions older than duration (e.g. 1h, 30m)")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(w, wErr, gf, usage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}

	var minAge time.Duration
	if *olderThan != "" {
		d, err := time.ParseDuration(*olderThan)
		if err != nil || d <= 0 {
			msg := "--older-than must be a positive duration"
			if err != nil {
				msg = fmt.Sprintf("invalid --older-than: %v", err)
			}
			return returnOperationError(w, wErr, gf, version,
				ExitUsage, "invalid_older_than", fmt.Errorf("%s", msg),
				map[string]any{"older_than": *olderThan},
				"%s", msg)
		}
		minAge = d
	}

	if svc == nil {
		var code int
		svc, code = initServicesOrFail(w, wErr, gf, version)
		if code >= 0 {
			return code
		}
	}

	rows, err := svc.QuerySessionRows(svc.TmuxOpts)
	if err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "prune_failed", err, nil,
			"failed to scan sessions: %v", err)
	}

	wsIDs, err := svc.Store.List()
	if err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "prune_failed", err, nil,
			"failed to list workspaces: %v", err)
	}

	candidates := findPruneCandidates(rows, wsIDs, minAge, time.Now())

	if !*yes {
		result := pruneResult{
			DryRun: true,
			Pruned: candidates,
			Total:  len(candidates),
			Errors: []string{},
		}
		if gf.JSON {
			PrintJSON(w, result, version)
			return ExitOK
		}
		PrintHuman(w, func(w io.Writer) {
			if len(candidates) == 0 {
				fmt.Fprintln(w, "Nothing to prune.")
				return
			}
			fmt.Fprintf(w, "Would prune %d session(s) (pass --yes to confirm):\n", len(candidates))
			for _, c := range candidates {
				fmt.Fprintf(w, "  %-45s (%s, %s old)\n", c.Session, humanReason(c.Reason), formatAge(c.AgeSeconds))
			}
		})
		return ExitOK
	}

	// Actually prune.
	var pruned []pruneEntry
	var errs []string
	for _, c := range candidates {
		if err := tmuxKillSession(c.Session, svc.TmuxOpts); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Session, err))
			continue
		}
		pruned = append(pruned, c)
	}

	result := pruneResult{
		DryRun: false,
		Pruned: pruned,
		Total:  len(pruned),
		Errors: errs,
	}
	if result.Errors == nil {
		result.Errors = []string{}
	}

	exitCode := ExitOK
	if len(errs) > 0 {
		exitCode = ExitInternalError
	}

	if gf.JSON {
		if len(errs) > 0 {
			ReturnError(w, "prune_partial_failed",
				fmt.Sprintf("pruned %d session(s) but %d failed", len(pruned), len(errs)),
				map[string]any{"pruned": pruned, "errors": errs}, version)
		} else {
			PrintJSON(w, result, version)
		}
		return exitCode
	}

	PrintHuman(w, func(w io.Writer) {
		if len(pruned) == 0 && len(errs) == 0 {
			fmt.Fprintln(w, "Nothing to prune.")
			return
		}
		if len(pruned) > 0 {
			fmt.Fprintf(w, "Pruned %d session(s):\n", len(pruned))
			for _, p := range pruned {
				fmt.Fprintf(w, "  %-45s (%s, %s old)\n", p.Session, humanReason(p.Reason), formatAge(p.AgeSeconds))
			}
		}
		for _, e := range errs {
			fmt.Fprintf(w, "Error: %s\n", e)
		}
	})
	return exitCode
}

// --- routing ---

func routeSession(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return routeSubcommand(w, wErr, gf, args, version, "session", []subcommand{
		{names: []string{"list", "ls"}, handler: cmdSessionList},
		{names: []string{"prune"}, handler: cmdSessionPrune},
	})
}
