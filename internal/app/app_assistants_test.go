package app

import (
	"reflect"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
)

// TestDefaultAssistantName pins the canonical default assistant the App falls
// back to. It must mirror data.DefaultAssistant exactly so the app and the data
// layer never disagree about which agent to launch by default.
func TestDefaultAssistantName(t *testing.T) {
	t.Run("returns the data-layer canonical default", func(t *testing.T) {
		app := &App{}
		if got := app.defaultAssistantName(); got != data.DefaultAssistant {
			t.Fatalf("defaultAssistantName() = %q, want %q", got, data.DefaultAssistant)
		}
	})

	t.Run("is independent of any configured assistants", func(t *testing.T) {
		// Even when the config advertises an entirely different roster, the
		// default name is sourced from the data layer, not from config.
		app := &App{
			config: &config.Config{
				Assistants: map[string]config.AssistantConfig{
					"codex": {Command: "codex"},
				},
			},
		}
		if got := app.defaultAssistantName(); got != data.DefaultAssistant {
			t.Fatalf("defaultAssistantName() with custom config = %q, want %q", got, data.DefaultAssistant)
		}
	})

	t.Run("is stable across repeated calls", func(t *testing.T) {
		app := &App{}
		first := app.defaultAssistantName()
		second := app.defaultAssistantName()
		if first != second {
			t.Fatalf("defaultAssistantName() not stable: first=%q second=%q", first, second)
		}
	})
}

// TestAssistantNames covers the branching in assistantNames: it prefers the
// ordered names reported by config, but falls back to a single-element slice
// holding the default assistant whenever config is absent or yields no names.
func TestAssistantNames(t *testing.T) {
	tests := []struct {
		name string
		app  *App
		want []string
	}{
		{
			name: "nil config falls back to default",
			app:  &App{},
			want: []string{data.DefaultAssistant},
		},
		{
			name: "empty assistant map falls back to default",
			// config is non-nil but AssistantNames() returns nil for an
			// empty map, exercising the len(names) == 0 fallback branch.
			app: &App{config: &config.Config{
				Assistants: map[string]config.AssistantConfig{},
			}},
			want: []string{data.DefaultAssistant},
		},
		{
			name: "single configured assistant is returned verbatim",
			app: &App{config: &config.Config{
				Assistants: map[string]config.AssistantConfig{
					"codex": {Command: "codex"},
				},
			}},
			want: []string{"codex"},
		},
		{
			name: "preferred order is honored over map iteration order",
			// AssistantNames orders known agents by the canonical preferred
			// order. claude precedes codex regardless of map insertion order.
			app: &App{config: &config.Config{
				Assistants: map[string]config.AssistantConfig{
					"codex":  {Command: "codex"},
					"claude": {Command: "claude"},
				},
			}},
			want: []string{"claude", "codex"},
		},
		{
			name: "unknown extras follow known agents in sorted order",
			// Names not present in the preferred order are appended after the
			// known ones, sorted lexicographically among themselves.
			app: &App{config: &config.Config{
				Assistants: map[string]config.AssistantConfig{
					"claude": {Command: "claude"},
					"zeta":   {Command: "zeta"},
					"alpha":  {Command: "alpha"},
				},
			}},
			want: []string{"claude", "alpha", "zeta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.app.assistantNames()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("assistantNames() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestAssistantNamesNeverEmpty guards the invariant the callers rely on: the
// returned slice always has at least one element, so the UI never has to render
// an empty assistant picker.
func TestAssistantNamesNeverEmpty(t *testing.T) {
	cases := []struct {
		name string
		app  *App
	}{
		{name: "no config", app: &App{}},
		{name: "empty config", app: &App{config: &config.Config{}}},
		{name: "empty assistant map", app: &App{config: &config.Config{
			Assistants: map[string]config.AssistantConfig{},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.app.assistantNames()
			if len(got) == 0 {
				t.Fatalf("assistantNames() returned empty slice for %s", tc.name)
			}
			if got[0] == "" {
				t.Fatalf("assistantNames() first element empty for %s: %#v", tc.name, got)
			}
		})
	}
}

// TestIsKnownAssistant exercises the membership check. The function trims input,
// treats blank names as unknown, and is permissive (returns true) whenever the
// config carries no assistant roster to check against.
func TestIsKnownAssistant(t *testing.T) {
	roster := func() *config.Config {
		return &config.Config{Assistants: map[string]config.AssistantConfig{
			"claude": {Command: "claude"},
			"codex":  {Command: "codex"},
		}}
	}

	tests := []struct {
		name      string
		app       *App
		assistant string
		want      bool
	}{
		{name: "empty name is never known", app: &App{config: roster()}, assistant: "", want: false},
		{name: "whitespace-only name is never known", app: &App{config: roster()}, assistant: "   ", want: false},
		{name: "nil app is permissive for non-empty name", app: nil, assistant: "anything", want: true},
		{name: "nil config is permissive for non-empty name", app: &App{}, assistant: "anything", want: true},
		{
			name:      "empty roster is permissive for non-empty name",
			app:       &App{config: &config.Config{Assistants: map[string]config.AssistantConfig{}}},
			assistant: "anything",
			want:      true,
		},
		{name: "configured assistant is known", app: &App{config: roster()}, assistant: "claude", want: true},
		{name: "surrounding whitespace is trimmed before lookup", app: &App{config: roster()}, assistant: "  codex  ", want: true},
		{name: "unconfigured assistant is unknown", app: &App{config: roster()}, assistant: "gemini", want: false},
		{name: "nil app still rejects empty name", app: nil, assistant: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.isKnownAssistant(tt.assistant); got != tt.want {
				t.Fatalf("isKnownAssistant(%q) = %v, want %v", tt.assistant, got, tt.want)
			}
		})
	}
}

// TestAssistantNamesMatchesConfigOrdering asserts assistantNames is a faithful
// pass-through of Config.AssistantNames when config supplies any names, rather
// than re-deriving or re-sorting the roster itself.
func TestAssistantNamesMatchesConfigOrdering(t *testing.T) {
	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"codex":  {Command: "codex"},
			"claude": {Command: "claude"},
			"custom": {Command: "custom"},
		},
	}
	app := &App{config: cfg}

	got := app.assistantNames()
	want := cfg.AssistantNames()
	if len(want) == 0 {
		t.Fatal("test precondition failed: config produced no assistant names")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assistantNames() = %#v, want config.AssistantNames() = %#v", got, want)
	}
}
