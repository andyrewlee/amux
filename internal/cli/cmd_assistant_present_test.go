package cli

import (
	"encoding/json"
	"testing"
)

func TestAssistantPresentTransform_AugmentsSelectedChannel(t *testing.T) {
	input := []byte(`{"message":"Build complete","quick_actions":[{"id":"status","label":"Status","command":"amux status","prompt":"Check status"}],"channel":{"message":"Build complete","chunks_meta":[{"index":1,"total":1,"text":"Build complete"}]}}`)

	output := assistantPresentTransform(input, "teams")

	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	assistantUX, ok := payload["assistant_ux"].(map[string]any)
	if !ok {
		t.Fatalf("assistant_ux missing or wrong type: %T", payload["assistant_ux"])
	}
	if got, _ := assistantUX["selected_channel"].(string); got != "msteams" {
		t.Fatalf("selected_channel = %q, want %q", got, "msteams")
	}
	presentation, ok := assistantUX["presentation"].(map[string]any)
	if !ok {
		t.Fatalf("presentation missing or wrong type: %T", assistantUX["presentation"])
	}
	suggestedActions, ok := presentation["suggested_actions"].([]any)
	if !ok || len(suggestedActions) != 1 {
		t.Fatalf("suggested_actions = %#v, want len=1", presentation["suggested_actions"])
	}
}

func TestAssistantPresentTransform_PassesThroughInvalidJSON(t *testing.T) {
	input := []byte("not json\n")
	if got := string(assistantPresentTransform(input, "slack")); got != string(input) {
		t.Fatalf("transform invalid json = %q, want %q", got, string(input))
	}
}

func TestAssistantPresentTransform_PassesThroughWhitespace(t *testing.T) {
	input := []byte(" \n\t")
	if got := string(assistantPresentTransform(input, "slack")); got != string(input) {
		t.Fatalf("transform whitespace = %q, want %q", got, string(input))
	}
}
