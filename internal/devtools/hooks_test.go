package devtools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookSkipFlags executes the real .githooks scripts against stub
// git/make/go/tmux commands and asserts each AMUX_SKIP_* flag removes only
// its named gate. A failure here means a flag silently bypasses more (or
// less) than CONTRIBUTING.md promises.
func TestHookSkipFlags(t *testing.T) {
	type expectation struct {
		desc    string
		prefix  []string
		present bool
	}
	cases := []struct {
		name    string
		hook    string
		env     map[string]string
		staged  []string
		expects []expectation
	}{
		{
			name:   "pre-commit no flags runs every gate",
			hook:   "pre-commit",
			staged: []string{"pkg/ok.go"},
			expects: []expectation{
				{"repo root lookup", []string{"git", "rev-parse", "--show-toplevel"}, true},
				{"gofumpt check", []string{"make", "fmt-check"}, true},
				{"golangci-lint", []string{"make", "lint"}, true},
				{"lint config drift", []string{"make", "lint-config-drift", "check-fmt-config"}, true},
				{"staged file guard", []string{"git", "diff", "--cached", "--name-only"}, true},
			},
		},
		{
			name:   "pre-commit skip-lint removes only golangci-lint",
			hook:   "pre-commit",
			env:    map[string]string{"AMUX_SKIP_LINT": "1"},
			staged: []string{"pkg/ok.go"},
			expects: []expectation{
				{"gofumpt check", []string{"make", "fmt-check"}, true},
				{"golangci-lint skipped", []string{"make", "lint"}, false},
				{"lint config drift retained", []string{"make", "lint-config-drift", "check-fmt-config"}, true},
				{"staged file guard retained", []string{"git", "diff", "--cached", "--name-only"}, true},
			},
		},
		{
			name:   "pre-commit skip-harness has no effect",
			hook:   "pre-commit",
			env:    map[string]string{"AMUX_SKIP_HARNESS": "1"},
			staged: []string{"pkg/ok.go"},
			expects: []expectation{
				{"gofumpt check", []string{"make", "fmt-check"}, true},
				{"golangci-lint", []string{"make", "lint"}, true},
				{"lint config drift", []string{"make", "lint-config-drift", "check-fmt-config"}, true},
			},
		},
		{
			name:   "pre-commit both flags still runs retained gates",
			hook:   "pre-commit",
			env:    map[string]string{"AMUX_SKIP_LINT": "1", "AMUX_SKIP_HARNESS": "1"},
			staged: []string{"pkg/ok.go"},
			expects: []expectation{
				{"gofumpt check", []string{"make", "fmt-check"}, true},
				{"golangci-lint skipped", []string{"make", "lint"}, false},
				{"lint config drift retained", []string{"make", "lint-config-drift", "check-fmt-config"}, true},
				{"staged file guard retained", []string{"git", "diff", "--cached", "--name-only"}, true},
			},
		},
		{
			name: "pre-push no flags runs every gate",
			hook: "pre-push",
			expects: []expectation{
				{"repo root lookup", []string{"git", "rev-parse", "--show-toplevel"}, true},
				{"strict lint", []string{"make", "lint-strict-base"}, true},
				{"harness run", []string{"go", "run", "./cmd/amux-harness"}, true},
				{"race smoke", []string{"go", "test", "-race"}, true},
				{"e2e tests", []string{"go", "test", "./internal/e2e", "-count=1"}, true},
				{"tmux probe", []string{"tmux", "-L"}, true},
			},
		},
		{
			name: "pre-push skip-lint removes only strict lint",
			hook: "pre-push",
			env:  map[string]string{"AMUX_SKIP_LINT": "1"},
			expects: []expectation{
				{"strict lint skipped", []string{"make", "lint-strict-base"}, false},
				{"harness run retained", []string{"go", "run", "./cmd/amux-harness"}, true},
				{"race smoke retained", []string{"go", "test", "-race"}, true},
				{"e2e tests retained", []string{"go", "test", "./internal/e2e", "-count=1"}, true},
				{"tmux probe retained", []string{"tmux", "-L"}, true},
			},
		},
		{
			name: "pre-push skip-harness removes only the harness",
			hook: "pre-push",
			env:  map[string]string{"AMUX_SKIP_HARNESS": "1"},
			expects: []expectation{
				{"strict lint retained", []string{"make", "lint-strict-base"}, true},
				{"harness skipped", []string{"go", "run", "./cmd/amux-harness"}, false},
				{"race smoke retained", []string{"go", "test", "-race"}, true},
				{"e2e tests retained", []string{"go", "test", "./internal/e2e", "-count=1"}, true},
				{"tmux probe retained", []string{"tmux", "-L"}, true},
			},
		},
		{
			name: "pre-push skip-race removes only the race smoke",
			hook: "pre-push",
			env:  map[string]string{"AMUX_SKIP_RACE": "1"},
			expects: []expectation{
				{"strict lint retained", []string{"make", "lint-strict-base"}, true},
				{"harness run retained", []string{"go", "run", "./cmd/amux-harness"}, true},
				{"race smoke skipped", []string{"go", "test", "-race"}, false},
				{"e2e tests retained", []string{"go", "test", "./internal/e2e", "-count=1"}, true},
				{"tmux probe retained", []string{"tmux", "-L"}, true},
			},
		},
		{
			name: "pre-push both flags keep e2e and tmux probe",
			hook: "pre-push",
			env:  map[string]string{"AMUX_SKIP_LINT": "1", "AMUX_SKIP_HARNESS": "1"},
			expects: []expectation{
				{"strict lint skipped", []string{"make", "lint-strict-base"}, false},
				{"harness skipped", []string{"go", "run", "./cmd/amux-harness"}, false},
				{"race smoke retained", []string{"go", "test", "-race"}, true},
				{"e2e tests retained", []string{"go", "test", "./internal/e2e", "-count=1"}, true},
				{"tmux probe retained", []string{"tmux", "-L"}, true},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newHookFixture(t, true)
			if len(tc.staged) > 0 {
				fx.stageFiles(t, tc.staged...)
			}
			out, code := fx.runHook(t, tc.hook, tc.env)
			if code != 0 {
				t.Fatalf("%s exited %d, want 0\noutput:\n%s\ninvocations:\n%s", tc.hook, code, out, fx.logDump(t))
			}
			for _, exp := range tc.expects {
				got := fx.sawPrefix(t, exp.prefix...)
				if got != exp.present {
					t.Errorf("%s: invocation %v present=%v, want %v\ninvocations:\n%s",
						exp.desc, exp.prefix, got, exp.present, fx.logDump(t))
				}
			}
		})
	}
}

// TestHookBaseRefForwarding pins the AMUX_LINT_BASE_REF contract: the value
// must reach `make lint-strict-base` as BASE_REF=<value>. It also pins
// REQUIRE_BASE=1 on the same invocation — a push implies a remote exists, so
// an unresolvable base must fail rather than fall back to a lint of only
// uncommitted changes.
func TestHookBaseRefForwarding(t *testing.T) {
	fx := newHookFixture(t, true)
	out, code := fx.runHook(t, "pre-push", map[string]string{"AMUX_LINT_BASE_REF": "release/1.2"})
	if code != 0 {
		t.Fatalf("pre-push exited %d\n%s", code, out)
	}
	if !fx.sawPrefix(t, "make", "lint-strict-base") {
		t.Fatalf("lint-strict-base not invoked\n%s", fx.logDump(t))
	}
	if !fx.sawAnyField(t, "BASE_REF=release/1.2") {
		t.Errorf("BASE_REF=release/1.2 not forwarded to make\n%s", fx.logDump(t))
	}
	if !fx.sawAnyField(t, "REQUIRE_BASE=1") {
		t.Errorf("REQUIRE_BASE=1 not forwarded to make\n%s", fx.logDump(t))
	}
}

// TestHookRetainedGatesFail proves that skipping one gate does not shield the
// others: a stub failure in any retained check must still exit nonzero.
func TestHookRetainedGatesFail(t *testing.T) {
	cases := []struct {
		name string
		hook string
		env  map[string]string
	}{
		{
			"pre-commit lint skipped but fmt-check fails", "pre-commit",
			map[string]string{"AMUX_SKIP_LINT": "1", "STUB_FAIL_MAKE": "fmt-check"},
		},
		{
			"pre-commit lint skipped but drift check fails", "pre-commit",
			map[string]string{"AMUX_SKIP_LINT": "1", "STUB_FAIL_MAKE": "lint-config-drift"},
		},
		{
			"pre-push harness skipped but strict lint fails", "pre-push",
			map[string]string{"AMUX_SKIP_HARNESS": "1", "STUB_FAIL_MAKE": "lint-strict-base"},
		},
		{
			"pre-push harness skipped but e2e fails", "pre-push",
			map[string]string{"AMUX_SKIP_HARNESS": "1", "STUB_FAIL_GO": "test"},
		},
		{
			// "-race" only matches the race-smoke argv — e2e runs without it.
			"pre-push race smoke failure fails the push", "pre-push",
			map[string]string{"STUB_FAIL_GO": "-race"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newHookFixture(t, true)
			out, code := fx.runHook(t, tc.hook, tc.env)
			if code == 0 {
				t.Fatalf("%s exited 0 despite retained gate failure\noutput:\n%s\ninvocations:\n%s",
					tc.hook, out, fx.logDump(t))
			}
		})
	}
}

// TestHookMissingTmuxWarning keeps the anti-false-green warning reachable: a
// usable push environment without tmux must still warn after e2e "passes" —
// and must keep warning when the harness gate is skipped.
func TestHookMissingTmuxWarning(t *testing.T) {
	for _, env := range []map[string]string{
		{},
		{"AMUX_SKIP_HARNESS": "1"},
	} {
		fx := newHookFixture(t, false) // no tmux stub: command -v fails
		out, code := fx.runHook(t, "pre-push", env)
		if code != 0 {
			t.Fatalf("pre-push exited %d with env %v, want warning-only success\n%s", code, env, out)
		}
		if !strings.Contains(out, "WARNING: e2e tests SKIPPED") {
			t.Errorf("missing-tmux warning absent with env %v\noutput:\n%s", env, out)
		}
	}
}

// TestHookStagedFilenameWithSpaces drives the real NUL-delimited
// wc/awk length guard with a staged path containing spaces and enough lines
// to trip the 500-line cap — even when lint is skipped.
func TestHookStagedFilenameWithSpaces(t *testing.T) {
	fx := newHookFixture(t, true)
	rel := filepath.Join("dir with space", "big_file.go")
	fx.stageFiles(t, rel)
	var b strings.Builder
	for i := 0; i < 501; i++ {
		b.WriteString("package x // padding line to exceed the cap\n")
	}
	if err := os.WriteFile(filepath.Join(fx.repoDir, rel), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write oversized file: %v", err)
	}

	out, code := fx.runHook(t, "pre-commit", map[string]string{"AMUX_SKIP_LINT": "1"})
	if code == 0 {
		t.Fatalf("pre-commit exited 0 for a 501-line staged file with spaces in the path\noutput:\n%s", out)
	}
	if !strings.Contains(out, "501") {
		t.Errorf("expected the guard to report the line count, got:\n%s", out)
	}
}
