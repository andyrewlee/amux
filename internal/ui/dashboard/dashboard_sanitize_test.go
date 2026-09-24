package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
)

// TestDashboardSanitizesWorkspaceAndProjectNames proves a filesystem-derived
// name can't carry escapes or fabricate rows into the dashboard frame.
func TestDashboardSanitizesWorkspaceAndProjectNames(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m.SetProjects([]data.Project{{
		Name: "proj\x1b[8mX",
		Path: "/repo",
		Workspaces: []data.Workspace{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"},
			{Name: "bad\x1b[8m\nFAKEROW", Branch: "b", Repo: "/repo", Root: "/repo/.amux/ws/bad"},
		},
	}})

	raw := m.View()
	if strings.Contains(raw, "\x1b[8m") {
		t.Fatalf("name escape reached the frame: %q", raw)
	}
	plain := ansi.Strip(raw)
	// Sanitized names are flattened onto their own row — the injected
	// newline can't fabricate a standalone "FAKEROW" row.
	for line := range strings.Lines(plain) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "FAKEROW" {
			t.Fatalf("injected newline fabricated a row: %q", plain)
		}
	}
	if !strings.Contains(plain, "badFAKEROW") {
		t.Fatalf("sanitized workspace name missing, got:\n%s", plain)
	}
}
