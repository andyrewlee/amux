package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyRunGlobalsAppliesAndRestores(t *testing.T) {
	prevTimeout := setCLITmuxTimeoutOverride(0)
	defer setCLITmuxTimeoutOverride(prevTimeout)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	targetWD := t.TempDir()
	restore, err := applyRunGlobals(GlobalFlags{
		Cwd:     targetWD,
		Timeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("applyRunGlobals() error = %v", err)
	}

	gotWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() after apply error = %v", err)
	}
	if gotWD != targetWD {
		gotCanonical, err := filepath.EvalSymlinks(gotWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(got cwd) error = %v", err)
		}
		wantCanonical, err := filepath.EvalSymlinks(targetWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(target cwd) error = %v", err)
		}
		if gotCanonical != wantCanonical {
			t.Fatalf("cwd after apply = %q, want %q", gotWD, targetWD)
		}
	}
	if got := currentCLITmuxTimeoutOverride(); got != 250*time.Millisecond {
		t.Fatalf("timeout override after apply = %v, want %v", got, 250*time.Millisecond)
	}

	restore()

	gotWD, err = os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() after restore error = %v", err)
	}
	if gotWD != originalWD {
		gotCanonical, err := filepath.EvalSymlinks(gotWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(got restored cwd) error = %v", err)
		}
		wantCanonical, err := filepath.EvalSymlinks(originalWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(original cwd) error = %v", err)
		}
		if gotCanonical != wantCanonical {
			t.Fatalf("cwd after restore = %q, want %q", gotWD, originalWD)
		}
	}
	if got := currentCLITmuxTimeoutOverride(); got != 0 {
		t.Fatalf("timeout override after restore = %v, want 0", got)
	}
}

func TestApplyRunGlobalsInvalidCwdRestoresTimeout(t *testing.T) {
	prevTimeout := setCLITmuxTimeoutOverride(0)
	defer setCLITmuxTimeoutOverride(prevTimeout)

	_, err := applyRunGlobals(GlobalFlags{
		Cwd:     filepath.Join(t.TempDir(), "missing"),
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatalf("expected error for invalid cwd")
	}

	if got := currentCLITmuxTimeoutOverride(); got != 0 {
		t.Fatalf("timeout override after invalid cwd = %v, want 0", got)
	}
}
