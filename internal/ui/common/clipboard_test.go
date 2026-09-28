package common

import (
	"errors"
	"strings"
	"testing"
)

// stubClipboardTools swaps the tool-detection seams for the duration of a
// test: present is the set of tool names LookPath would find; runErr maps a
// tool name to its Run error (absent entry = success). It returns the list of
// tool invocations observed.
func stubClipboardTools(t *testing.T, present map[string]bool, runErr map[string]error) *[]string {
	t.Helper()
	var calls []string

	oldExists, oldRun := clipboardToolExists, runClipboardTool
	clipboardToolExists = func(name string) bool { return present[name] }
	runClipboardTool = func(name string, args []string, text string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if text != "hello" {
			t.Errorf("clipboard tool got text %q, want %q", text, "hello")
		}
		return runErr[name]
	}
	t.Cleanup(func() {
		clipboardToolExists, runClipboardTool = oldExists, oldRun
	})
	return &calls
}

func TestCopyWithToolNoToolsAvailable(t *testing.T) {
	stubClipboardTools(t, map[string]bool{}, nil)
	err := copyWithTool("hello")
	if err == nil {
		t.Fatal("expected error when no clipboard tool exists")
	}
	if !strings.Contains(err.Error(), "no clipboard tool found") {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, name := range []string{"wl-copy", "xclip", "xsel", "pbcopy", "clip"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should list checked tool %q: %v", name, err)
		}
	}
}

func TestCopyWithToolPrefersWayland(t *testing.T) {
	calls := stubClipboardTools(t,
		map[string]bool{"wl-copy": true, "xclip": true},
		nil)
	if err := copyWithTool("hello"); err != nil {
		t.Fatalf("copyWithTool: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != "wl-copy " {
		t.Fatalf("expected single wl-copy invocation, got %v", *calls)
	}
}

func TestCopyWithToolPassesToolArgs(t *testing.T) {
	calls := stubClipboardTools(t, map[string]bool{"xclip": true}, nil)
	if err := copyWithTool("hello"); err != nil {
		t.Fatalf("copyWithTool: %v", err)
	}
	want := "xclip -selection clipboard"
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("expected %q, got %v", want, *calls)
	}
}

func TestCopyWithToolFallsThroughOnRunFailure(t *testing.T) {
	calls := stubClipboardTools(t,
		map[string]bool{"wl-copy": true, "xsel": true},
		map[string]error{"wl-copy": errors.New("wayland display gone")})
	if err := copyWithTool("hello"); err != nil {
		t.Fatalf("expected fallback to xsel, got %v", err)
	}
	if len(*calls) != 2 || (*calls)[1] != "xsel --clipboard --input" {
		t.Fatalf("expected wl-copy then xsel, got %v", *calls)
	}
}

func TestCopyWithToolAllFoundAllFail(t *testing.T) {
	stubClipboardTools(t,
		map[string]bool{"wl-copy": true, "clip": true},
		map[string]error{"wl-copy": errors.New("a"), "clip": errors.New("b")})
	err := copyWithTool("hello")
	if err == nil {
		t.Fatal("expected error when every tool fails")
	}
	if !strings.Contains(err.Error(), "tried: wl-copy, clip") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("error should name tried tools and wrap last failure, got: %v", err)
	}
}
