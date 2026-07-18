package process

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ProcessInfo describes one live process observed by Snapshot.
type ProcessInfo struct {
	PID     int
	PGID    int
	PPID    int
	CPU     float64 // instantaneous %CPU as reported by ps
	Command string
	// StartedAt is the process start time (zero when unparseable). Together
	// with PID/PGID it identifies a process across snapshots: a recycled PID
	// has a different start time.
	StartedAt time.Time
}

// lstartLayout parses ps's `lstart` column under LC_ALL=C, whitespace-
// normalized ("Sun Jul 13 13:00:00 2026" or "Sun Jul  3 ..." → "Jul 3").
const lstartLayout = "Mon Jan 2 15:04:05 2006"

// parsePSLines parses `ps -axo pid=,pgid=,ppid=,pcpu=,lstart=,command=`
// output: four numeric columns, five start-time tokens, then the raw command
// line (which may itself contain spaces). Unparseable lines are skipped
// rather than failing the snapshot — ps output can interleave kernel tasks
// with no command.
func parsePSLines(out string) []ProcessInfo {
	lines := strings.Split(out, "\n")
	procs := make([]ProcessInfo, 0, len(lines))
	for _, line := range lines {
		var cols [9]string
		rest := line
		ok := true
		for i := range cols {
			rest = strings.TrimLeft(rest, " \t")
			cut := strings.IndexAny(rest, " \t")
			if cut < 0 {
				ok = false
				break
			}
			cols[i], rest = rest[:cut], rest[cut:]
		}
		command := strings.TrimSpace(rest)
		if !ok || command == "" {
			continue
		}
		pid, err1 := strconv.Atoi(cols[0])
		pgid, err2 := strconv.Atoi(cols[1])
		ppid, err3 := strconv.Atoi(cols[2])
		cpu, err4 := strconv.ParseFloat(strings.TrimSuffix(cols[3], "%"), 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		// A zero StartedAt (bad lstart) degrades identity checks, never the
		// snapshot itself.
		startedAt, _ := time.ParseInLocation(lstartLayout, strings.Join(cols[4:9], " "), time.Local)
		procs = append(procs, ProcessInfo{
			PID: pid, PGID: pgid, PPID: ppid, CPU: cpu,
			Command: command, StartedAt: startedAt,
		})
	}
	return procs
}

// Descendants returns the transitive children of rootPID within snap,
// excluding rootPID itself. Order is stable (by PID).
func Descendants(snap []ProcessInfo, rootPID int) []ProcessInfo {
	children := make(map[int][]ProcessInfo, len(snap))
	for _, p := range snap {
		children[p.PPID] = append(children[p.PPID], p)
	}
	var out []ProcessInfo
	queue := []int{rootPID}
	seen := map[int]bool{rootPID: true}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children[pid] {
			if seen[child.PID] {
				continue
			}
			seen[child.PID] = true
			out = append(out, child)
			queue = append(queue, child.PID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// ReferencingPath returns the processes whose command line references path
// itself or a location under it. The match is boundary-aware so /a/b does not
// match /a/bc.
func ReferencingPath(snap []ProcessInfo, path string) []ProcessInfo {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return nil
	}
	var out []ProcessInfo
	for _, p := range snap {
		if commandReferencesPath(p.Command, path) {
			out = append(out, p)
		}
	}
	return out
}

// pathBoundaryChars terminate a path embedded in a command line. Both the
// matcher (commandReferencesPath) and the extractor (referencedWorkspaceRoots
// in reaper.go) MUST use this same set: if they disagree, a process can
// "reference" a root that extraction never finds, or vice versa, and orphans
// are silently skipped.
const pathBoundaryChars = " \t\"';:"

func commandReferencesPath(command, path string) bool {
	for rest := command; ; {
		idx := strings.Index(rest, path)
		if idx < 0 {
			return false
		}
		boundary := idx + len(path)
		if boundary >= len(rest) {
			return true
		}
		if c := rest[boundary]; c == '/' || strings.IndexByte(pathBoundaryChars, c) >= 0 {
			return true
		}
		rest = rest[boundary:]
	}
}

// sessionCommands are interactive processes that legitimately hold a
// workspace's directory: multiplexers, editors, IDE processes, and agent
// clients. Teardown and reaping must never treat these as managed services —
// killing a user's shell, editor, or running agent breaks the
// detach/reattach contract.
var sessionCommands = map[string]bool{
	"tmux": true, "ssh": true, "mosh-client": true,
	"vim": true, "nvim": true, "vi": true, "emacs": true, "nano": true,
	"less": true, "more": true, "man": true,
	"claude": true, "codex": true, "amux": true, "gemini": true, "aider": true,
	"code": true, "cursor": true, "zed": true, "subl": true, "electron": true,
}

var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"tcsh": true, "csh": true, "ksh": true,
}

// IsSessionCommand reports whether a command line looks like an interactive
// session process rather than a managed service workload. Shells need the
// finer split: a login/interactive shell ("-zsh", "zsh -l", tmux's
// "sh -lc ...; exec zsh -l" bootstrap) is a session, while "sh -c <cmd>" is a
// service wrapper — it is exactly how managed dev-server stacks are spawned.
// macOS app bundles (any ".app/" executable — editors, GUI apps launched from
// launchd with PPID 1) are always sessions. Ambiguous shell invocations
// default to session (never kill on doubt).
func IsSessionCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	if strings.HasPrefix(fields[0], "-") { // login shells report as "-zsh"
		return true
	}
	// macOS app bundle executables ("Visual Studio Code.app/...") contain
	// spaces, so check the whole command line rather than argv0. Erring
	// toward "session" is the safe direction for a never-kill guard.
	if strings.Contains(command, ".app/") {
		return true
	}
	name := strings.ToLower(filepath.Base(fields[0]))
	if sessionCommands[name] {
		return true
	}
	if !shellCommands[name] {
		return false
	}
	if len(fields) == 1 {
		return true
	}
	flags := fields[1]
	if !strings.HasPrefix(flags, "-") {
		return true
	}
	if strings.Contains(flags, "l") || strings.Contains(flags, "i") {
		return true
	}
	return !strings.Contains(flags, "c")
}

// GroupLeaders reduces procs to one representative per process group: the
// group leader when present, otherwise the lowest-PID member. Killing by
// group via these representatives covers every listed process exactly once.
func GroupLeaders(procs []ProcessInfo) []ProcessInfo {
	byGroup := make(map[int]ProcessInfo)
	for _, p := range procs {
		cur, ok := byGroup[p.PGID]
		if !ok || p.PID == p.PGID || (cur.PID != cur.PGID && p.PID < cur.PID) {
			byGroup[p.PGID] = p
		}
	}
	out := make([]ProcessInfo, 0, len(byGroup))
	for _, p := range byGroup {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PGID < out[j].PGID })
	return out
}
