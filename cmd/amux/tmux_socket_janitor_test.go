//go:build !windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// shortSocketDir returns a short-path temp dir suitable for unix sockets and
// points the janitor at it. Unix socket paths are length-limited (~104 bytes on
// macOS), so the deep `go test` temp dir under /var/folders is unusable here.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "amux-jtest-")
	if err != nil {
		t.Fatalf("mkdir temp socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	prev := tmuxSocketDirsFn
	tmuxSocketDirsFn = func() []string { return []string{dir} }
	t.Cleanup(func() { tmuxSocketDirsFn = prev })
	return dir
}

// listenUnix creates a live unix socket at path and registers its cleanup.
func listenUnix(t *testing.T, path string) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix %s: %v", path, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestCleanupStaleTestTmuxSockets_RemovesStaleTestSocket(t *testing.T) {
	dir := shortSocketDir(t)

	stale := filepath.Join(dir, "amux-test-dead")
	// Create a real socket inode, then close the listener so a dial is refused.
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close()
	// Closing a unix listener removes the file; recreate it as a socket inode
	// without an active listener so it presents as stale (dial is refused).
	if _, err := os.Stat(stale); os.IsNotExist(err) {
		ln2, err := net.Listen("unix", stale)
		if err != nil {
			t.Fatalf("relisten: %v", err)
		}
		// Detach the file from the listener: rename keeps the socket inode on disk
		// while the listener still holds (and will free) the original fd.
		tmp := stale + ".keep"
		if err := os.Rename(stale, tmp); err != nil {
			_ = ln2.Close()
			t.Fatalf("rename: %v", err)
		}
		_ = ln2.Close()
		if err := os.Rename(tmp, stale); err != nil {
			t.Fatalf("rename back: %v", err)
		}
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("stale socket missing before cleanup: %v", err)
	}

	cleanupStaleTestTmuxSockets()

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale test socket to be removed, stat err = %v", err)
	}
}

func TestCleanupStaleTestTmuxSockets_KeepsLiveTestSocket(t *testing.T) {
	dir := shortSocketDir(t)

	live := filepath.Join(dir, "amux-test-live")
	listenUnix(t, live) // active listener => dial succeeds => kept

	cleanupStaleTestTmuxSockets()

	if _, err := os.Stat(live); err != nil {
		t.Fatalf("expected live test socket to be kept, stat err = %v", err)
	}
}

func TestCleanupStaleTestTmuxSockets_IgnoresNonTestPrefix(t *testing.T) {
	dir := shortSocketDir(t)

	// A non-test socket with no listener: would be "stale" but must be skipped
	// because its name lacks the amux-test-/amux-e2e-check- prefix.
	other := filepath.Join(dir, "default")
	ln, err := net.Listen("unix", other)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	cleanupStaleTestTmuxSockets()

	if _, err := os.Stat(other); err != nil {
		t.Fatalf("expected non-test socket to be ignored/kept, stat err = %v", err)
	}
}

func TestCleanupStaleTestTmuxSockets_KeepsRegularFile(t *testing.T) {
	dir := shortSocketDir(t)

	// A regular file matching the prefix is not a socket and must be left alone.
	regular := filepath.Join(dir, "amux-test-regular")
	if err := os.WriteFile(regular, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupStaleTestTmuxSockets()

	if _, err := os.Stat(regular); err != nil {
		t.Fatalf("expected regular file to be kept, stat err = %v", err)
	}
}
