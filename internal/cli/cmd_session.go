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
	const usage = "Usage: amux session list [--json]"
	if len(args) > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(args, " ")))
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	rows, err := sessionQueryRows(svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "list_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to list sessions: %v", err)
		}
		return ExitInternalError
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
		if err != nil {
			if gf.JSON {
				ReturnError(w, "invalid_older_than", fmt.Sprintf("invalid --older-than: %v", err),
					map[string]any{"older_than": *olderThan}, version)
			} else {
				Errorf(wErr, "invalid --older-than: %v", err)
			}
			return ExitUsage
		}
		if d <= 0 {
			if gf.JSON {
				ReturnError(w, "invalid_older_than", "--older-than must be > 0",
					map[string]any{"older_than": *olderThan}, version)
			} else {
				Errorf(wErr, "--older-than must be > 0")
			}
			return ExitUsage
		}
		minAge = d
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	rows, err := sessionQueryRows(svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "prune_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to scan sessions: %v", err)
		}
		return ExitInternalError
	}

	wsIDs, err := svc.Store.List()
	if err != nil {
		if gf.JSON {
			ReturnError(w, "prune_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to list workspaces: %v", err)
		}
		return ExitInternalError
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
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", "Usage: amux session <list|prune> [flags]", nil, version)
		} else {
			fmt.Fprintln(wErr, "Usage: amux session <list|prune> [flags]")
		}
		return ExitUsage
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "list", "ls":
		return cmdSessionList(w, wErr, gf, subArgs, version)
	case "prune":
		return cmdSessionPrune(w, wErr, gf, subArgs, version)
	default:
		if gf.JSON {
			ReturnError(w, "unknown_command", "Unknown session subcommand: "+sub, nil, version)
		} else {
			fmt.Fprintf(wErr, "Unknown session subcommand: %s\n", sub)
		}
		return ExitUsage
	}
}
