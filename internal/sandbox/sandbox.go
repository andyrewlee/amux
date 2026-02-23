package sandbox

import (
	"fmt"
	"os"
	"strings"
)

// GenerateSBPL produces a macOS Seatbelt (SBPL) profile that restricts
// filesystem access while allowing normal CLI tool operation.
//
// Strategy (modeled after Anthropic's sandbox-runtime):
//   - (deny default) — deny everything by default
//   - Allow all file reads globally, then deny reads to sensitive paths
//   - Allow file writes ONLY to specific directories (workspace, config, tmp)
//   - Allow file-map-executable globally (needed by dyld for loading dynamic libraries)
//   - Allow network (Claude API), process execution, and minimal system operations
func GenerateSBPL(worktreeRoot, gitDir, claudeConfigDir string) string {
	home, _ := os.UserHomeDir()

	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n\n")

	// ── File reads ──────────────────────────────────────────────
	// Allow reads globally, then deny sensitive paths.
	b.WriteString(";; File reads — allow globally, deny sensitive paths\n")
	b.WriteString("(allow file-read*)\n")
	for _, p := range []string{
		home + "/.ssh",
		home + "/.gnupg",
		home + "/.aws",
		home + "/.docker",
		home + "/.kube",
	} {
		fmt.Fprintf(&b, "(deny file-read* (subpath %q))\n", p)
	}
	b.WriteString("\n")

	// ── Dynamic library loading ─────────────────────────────────
	// Required by dyld to memory-map executables and shared libraries.
	// Without this, any dynamically-linked program crashes on startup.
	b.WriteString(";; Dynamic library loading (dyld)\n")
	b.WriteString("(allow file-map-executable)\n\n")

	// ── File writes ─────────────────────────────────────────────
	// Deny-default already blocks all writes. Selectively allow.
	b.WriteString(";; File writes — workspace\n")
	fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n\n", worktreeRoot)

	if gitDir != "" {
		b.WriteString(";; File writes — git internals (commits, refs, objects)\n")
		fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n\n", gitDir)
	}

	if claudeConfigDir != "" {
		b.WriteString(";; File writes — Claude config dir (CLAUDE_CONFIG_DIR)\n")
		fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n\n", claudeConfigDir)
	}

	b.WriteString(";; File writes — /dev (stdout, stderr, /dev/null)\n")
	b.WriteString("(allow file-write* (regex #\"^/dev/\"))\n\n")

	b.WriteString(";; File writes — temp directories\n")
	b.WriteString("(allow file-write* (subpath \"/private/tmp\"))\n")
	b.WriteString("(allow file-write* (subpath \"/private/var/folders\"))\n\n")

	// ── Process execution ───────────────────────────────────────
	b.WriteString(";; Process execution\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-info*)\n")
	b.WriteString("(allow signal)\n\n")

	// ── Terminal ────────────────────────────────────────────────
	// Required for interactive TUI apps (e.g. Claude Code's setRawMode).
	b.WriteString(";; Terminal ioctl (raw mode, window size)\n")
	b.WriteString("(allow file-ioctl)\n\n")

	// ── System operations ───────────────────────────────────────
	b.WriteString(";; System operations\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow ipc-posix-shm*)\n\n")

	// ── Network ─────────────────────────────────────────────────
	b.WriteString(";; Network (Claude API calls)\n")
	b.WriteString("(allow network*)\n")

	return b.String()
}

// WrapCommand wraps an agent command string with sandbox-exec using the
// given SBPL profile file path.
func WrapCommand(agentCommand, sbplPath string) string {
	return fmt.Sprintf("sandbox-exec -f %s sh -lc %s", shellQuote(sbplPath), shellQuote(agentCommand))
}

// WriteTempProfile writes an SBPL profile string to a temporary file and
// returns the file path and a cleanup function that removes it.
func WriteTempProfile(sbpl string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "medusa-sandbox-*.sb")
	if err != nil {
		return "", nil, fmt.Errorf("create sandbox profile: %w", err)
	}
	if _, err := f.WriteString(sbpl); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("write sandbox profile: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("close sandbox profile: %w", err)
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// shellQuote wraps a value in single quotes for safe shell embedding.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
