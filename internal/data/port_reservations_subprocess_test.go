//go:build !windows

package data

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The port-reservation registry exists to serialize *separate amux processes*
// sharing one state home — same-process goroutine tests cannot prove that
// (flock semantics differ across the process boundary). These tests re-exec
// the test binary as helper children that each run Initialize+Reserve for a
// distinct workspace ID, then the parent asserts every committed interval is
// pairwise disjoint. See lock_test.go for the re-exec idiom's origin.

const (
	portResChildEnv   = "AMUX_PORTRES_CHILD"
	portResChildHome  = "AMUX_PORTRES_CHILD_HOME"
	portResChildID    = "AMUX_PORTRES_CHILD_ID"
	portResChildGuard = "AMUX_PORTRES_CHILD_GUARD" // "fail" forces a guard refusal
)

// TestPortReservationSubprocessHelper is the child-mode entrypoint: when the
// env vars are set it runs the real Initialize+Reserve path against the
// shared home and prints "base end" for the parent to collect. In the normal
// suite it is a no-op pass.
func TestPortReservationSubprocessHelper(t *testing.T) {
	if os.Getenv(portResChildEnv) == "" {
		return
	}
	home, id := os.Getenv(portResChildHome), os.Getenv(portResChildID)
	guard := func() error { return nil }
	if os.Getenv(portResChildGuard) == "fail" {
		guard = func() error { return errors.New("child guard refusal") }
	}
	s := NewPortReservationStore(home)
	if err := s.Initialize(guard); err != nil {
		fmt.Fprintf(os.Stderr, "initialize: %v\n", err)
		os.Exit(2)
	}
	base, end, err := s.Reserve(id, 6200, 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reserve: %v\n", err)
		os.Exit(3)
	}
	fmt.Fprintf(os.Stdout, "%d %d\n", base, end)
}

// TestPortReservations_CrossProcessDisjoint proves OS-level locking keeps two
// genuinely separate processes' committed intervals non-overlapping — the
// invariant the whole feature exists for.
func TestPortReservations_CrossProcessDisjoint(t *testing.T) {
	home := t.TempDir()
	const n = 4

	type outcome struct {
		base, end int
		err       error
	}
	outcomes := make([]outcome, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			base, end, err := runPortReservationChild(t, home, fmt.Sprintf("proc-%d", i), "")
			outcomes[i] = outcome{base: base, end: end, err: err}
		}(i)
	}
	wg.Wait()

	intervals := make(map[string][2]int, n)
	for i, o := range outcomes {
		if o.err != nil {
			t.Fatalf("child proc-%d: %v", i, o.err)
		}
		intervals[fmt.Sprintf("proc-%d", i)] = [2]int{o.base, o.end}
	}
	for id, iv := range intervals {
		for other, oiv := range intervals {
			if id != other && iv[0] <= oiv[1] && iv[1] >= oiv[0] {
				t.Fatalf("cross-process overlap: %q %v vs %q %v", id, iv, other, oiv)
			}
		}
	}
}

// TestPortReservations_CrossProcessInitContention races two children at
// first-adoption on an empty home: both must converge on the one committed
// envelope — the loser sees the winner's file rather than double-creating.
func TestPortReservations_CrossProcessInitContention(t *testing.T) {
	home := t.TempDir()

	type outcome struct {
		base, end int
		err       error
	}
	results := make([]outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			base, end, err := runPortReservationChild(t, home, fmt.Sprintf("init-%d", i), "")
			results[i] = outcome{base: base, end: end, err: err}
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("init child %d: %v", i, r.err)
		}
	}
	if results[0].base == results[1].base {
		t.Fatalf("both children committed base %d — registry was not shared", results[0].base)
	}
}

// TestPortReservations_CrossProcessGuardRefusal proves a child whose
// first-adoption guard refuses neither creates a registry nor blocks the
// parent's own guarded initialization.
func TestPortReservations_CrossProcessGuardRefusal(t *testing.T) {
	home := t.TempDir()
	if _, _, err := runPortReservationChild(t, home, "refused", "fail"); err == nil {
		t.Fatal("child with failing guard succeeded")
	}
	if _, err := os.Stat(filepath.Join(home, PortReservationRegistryFile)); !os.IsNotExist(err) {
		t.Fatal("refused child still created a registry")
	}
	// Parent can still adopt once the gate clears.
	s := NewPortReservationStore(home)
	if err := s.Initialize(nil); err != nil {
		t.Fatalf("parent Initialize after child refusal: %v", err)
	}
}

// runPortReservationChild re-execs the test binary in helper mode and parses
// its "base end" output line.
func runPortReservationChild(t *testing.T, home, id, guardMode string) (int, int, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPortReservationSubprocessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		portResChildEnv+"=1",
		portResChildHome+"="+home,
		portResChildID+"="+id,
		portResChildGuard+"="+guardMode,
	)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return 0, 0, fmt.Errorf("child: %w %s", err, stderr)
	}
	var base, end int
	if _, scanErr := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &base, &end); scanErr != nil {
		return 0, 0, fmt.Errorf("child output %q: %w", out, scanErr)
	}
	return base, end, nil
}
