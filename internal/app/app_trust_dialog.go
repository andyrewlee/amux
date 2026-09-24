package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// trustReviewManifest renders what a repo-scripts approval actually covers —
// the approved commands (labeled, sanitized, truncated) and the repo env key
// NAMES (never values: repo env may carry secrets, and a name like PATH or
// LD_PRELOAD is itself the warning signal). Returns "" when the config is
// absent/empty so the dialog keeps its one-line form.
func (a *App) trustReviewManifest(repo string) string {
	if a.workspaceService == nil || repo == "" {
		return ""
	}
	config, err := a.workspaceService.ScriptConfig(repo)
	if err != nil || config == nil {
		return ""
	}
	const maxCmdRunes = 80
	var lines []string
	for _, cmd := range config.SetupWorkspace {
		if cmd == "" {
			continue
		}
		lines = append(lines, "  setup: "+common.SanitizeDisplayText(cmd, maxCmdRunes))
	}
	for _, item := range []struct{ label, cmd string }{
		{"run", config.RunScript},
		{"archive", config.ArchiveScript},
		{"on-done", config.OnDoneScript},
	} {
		if item.cmd == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", item.label, common.SanitizeDisplayText(item.cmd, maxCmdRunes)))
	}
	if len(config.Env) > 0 {
		keys := make([]string, 0, len(config.Env))
		for k := range config.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// Key names are attacker-controlled JSON object keys — sanitize like
		// the commands so ESC/newline bytes cannot fabricate dialog lines or
		// conceal the real manifest. Sort on the raw keys: order is
		// deterministic either way and raw-sort keeps the stable ordering.
		sanitized := make([]string, 0, len(keys))
		for _, k := range keys {
			sanitized = append(sanitized, common.SanitizeDisplayText(k, maxCmdRunes))
		}
		lines = append(lines, "  env: "+strings.Join(sanitized, ", "))
	}
	return strings.Join(lines, "\n")
}

// repoScriptCommandsForTrust returns the repo-supplied commands from repo's
// .amux/workspaces.json (setup-workspace/run/archive/on-done — the same
// commands the trust gate hashes), best-effort and read-only, for the trust
// dialog's indirection warning. It never gates: a nil service, empty repo, or
// load error simply yields no commands (and therefore no warning). It does not
// hash and so cannot disagree with what the gate hashed.
func (a *App) repoScriptCommandsForTrust(repo string) []string {
	if a.workspaceService == nil || repo == "" {
		return nil
	}
	config, err := a.workspaceService.ScriptConfig(repo)
	if err != nil || config == nil {
		return nil
	}
	commands := append([]string(nil), config.SetupWorkspace...)
	if config.RunScript != "" {
		commands = append(commands, config.RunScript)
	}
	if config.ArchiveScript != "" {
		commands = append(commands, config.ArchiveScript)
	}
	if config.OnDoneScript != "" {
		commands = append(commands, config.OnDoneScript)
	}
	return commands
}

// scriptIndirectionWarning builds the trust dialog's advisory text about
// commands that reach into in-repo files the manifest hash cannot pin. It runs
// the shipped, already-tested detector (process.ReferencesInRepoFiles /
// CommandIsUnresolvable) over the repo-supplied commands and reports what it
// found. It returns "" only when the detector found neither a referenced file
// nor an unresolvable construct — which is explicitly NOT a guarantee the
// commands run no repo code (the detector's contract), so the empty case renders
// nothing rather than any reassurance. It never authorizes or blocks anything.
func scriptIndirectionWarning(commands []string, repoRoot string) string {
	var refs []string
	seen := make(map[string]struct{})
	unresolvable := false
	for _, cmd := range commands {
		if process.CommandIsUnresolvable(cmd) {
			unresolvable = true
		}
		for _, ref := range process.ReferencesInRepoFiles(cmd, repoRoot) {
			if _, dup := seen[ref]; dup {
				continue
			}
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
	}

	var lines []string
	if len(refs) > 0 {
		lines = append(lines, "Runs in-repo scripts amux can't re-verify after approval: "+strings.Join(refs, ", "))
	}
	if unresolvable {
		lines = append(lines, "One or more commands use variables/globs — amux can't list every file they run.")
	}
	return strings.Join(lines, "\n")
}
