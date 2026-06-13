//go:build !windows

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const staleSocketDialTimeout = 75 * time.Millisecond

// tmuxSocketDirsFn resolves the directories the janitor sweeps. It is a package
// var so tests can point it at a temp dir instead of the real tmux sockets.
var tmuxSocketDirsFn = tmuxSocketDirs

func cleanupStaleTestTmuxSockets() {
	removed := 0
	for _, dir := range tmuxSocketDirsFn() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "amux-test-") && !strings.HasPrefix(name, "amux-e2e-check-") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSocket == 0 {
				continue
			}
			socketPath := filepath.Join(dir, name)
			if isLiveUnixSocket(socketPath) {
				continue
			}
			if err := os.Remove(socketPath); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		logging.Info("Removed %d stale tmux test sockets", removed)
	}
}

func tmuxSocketDirs() []string {
	uid := os.Getuid()
	candidates := []string{
		filepath.Join("/tmp", fmt.Sprintf("tmux-%d", uid)),
		filepath.Join("/private/tmp", fmt.Sprintf("tmux-%d", uid)),
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return out
}

func isLiveUnixSocket(path string) bool {
	dialer := net.Dialer{Timeout: staleSocketDialTimeout}
	conn, err := dialer.Dial("unix", path)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
