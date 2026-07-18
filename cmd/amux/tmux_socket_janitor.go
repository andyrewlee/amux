//go:build !windows

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
			if !isTestTmuxSocketName(name) {
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
			if !isStaleUnixSocket(socketPath) {
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

func isTestTmuxSocketName(name string) bool {
	for _, prefix := range []string{
		"amux-test-",
		"amux-e2e-",
		"amux-gctest-",
		"amux-ptytest-",
		"amux-sidebar-test-",
		"amux-closeloop-check-",
		"amux-create-pipeline-",
		"amux-pre-push-e2e-check-",
		"amux-verify-loop-check-",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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

func isStaleUnixSocket(path string) bool {
	dialer := net.Dialer{Timeout: staleSocketDialTimeout}
	conn, err := dialer.Dial("unix", path)
	if err == nil {
		_ = conn.Close()
		return false
	}
	// Fail closed unless the kernel definitively says the socket has no listener
	// (or disappeared during the scan). A timeout can be a busy live server.
	return isDefinitivelyStaleSocketError(err)
}

func isDefinitivelyStaleSocketError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist)
}
