package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdAgentWatchRejectsNonPositiveIdleThreshold(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdAgentWatch(&w, &wErr, GlobalFlags{}, []string{"session-a", "--idle-threshold", "0s"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentWatch() code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(wErr.String(), "--idle-threshold must be > 0") {
		t.Fatalf("expected validation message, got %q", wErr.String())
	}
}

func TestCmdAgentWatchRejectsNonPositiveIdleThresholdJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdAgentWatch(&w, &wErr, GlobalFlags{JSON: true}, []string{"session-a", "--idle-threshold", "-1s"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentWatch() code = %d, want %d", code, ExitUsage)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "invalid_idle_threshold" {
		t.Fatalf("expected invalid_idle_threshold, got %#v", env.Error)
	}
}

func TestComputeNewLinesAppended(t *testing.T) {
	prev := []string{"a", "b", "c"}
	curr := []string{"a", "b", "c", "d", "e"}
	got := computeNewLines(prev, curr)
	want := []string{"d", "e"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestComputeNewLinesNoOverlap(t *testing.T) {
	prev := []string{"a", "b"}
	curr := []string{"x", "y", "z"}
	got := computeNewLines(prev, curr)
	// No overlap → return all current
	if len(got) != 3 {
		t.Fatalf("got %v, want all current lines", got)
	}
}

func TestComputeNewLinesShrunkNoAdditions(t *testing.T) {
	prev := []string{"a", "b"}
	curr := []string{"b"}
	got := computeNewLines(prev, curr)
	if len(got) != 0 {
		t.Fatalf("got %v, want 0 new lines", got)
	}
}

func TestComputeNewLinesShrunkTrailingBlankLine(t *testing.T) {
	prev := strings.Split("line1\n\n", "\n")
	curr := strings.Split("line1\n", "\n")
	got := computeNewLines(prev, curr)
	if len(got) != 0 {
		t.Fatalf("got %v, want 0 new lines", got)
	}
}

func TestComputeNewLinesEmptyPrevious(t *testing.T) {
	curr := []string{"a", "b"}
	got := computeNewLines(nil, curr)
	if len(got) != 2 {
		t.Fatalf("got %v, want %v", got, curr)
	}
}

func TestComputeNewLinesTrailingBlankLineAdded(t *testing.T) {
	prev := strings.Split("line1\n", "\n")
	curr := strings.Split("line1\n\n", "\n")
	got := computeNewLines(prev, curr)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("got %v, want [\"\"]", got)
	}
}
