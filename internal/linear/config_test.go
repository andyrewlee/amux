package linear

import (
	"encoding/json"
	"testing"
)

func TestBoardConfigUnmarshalStringMapping(t *testing.T) {
	jsonData := []byte(`{"board":{"columns":["Todo"],"stateMapping":"auto"}}`)
	cfg := DefaultConfig()
	if err := json.Unmarshal(jsonData, cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Board.StateMappingMode != "auto" {
		t.Fatalf("expected stateMappingMode auto, got %q", cfg.Board.StateMappingMode)
	}
	if len(cfg.Board.Columns) != 1 || cfg.Board.Columns[0] != "Todo" {
		t.Fatalf("expected columns to be parsed")
	}
}

func TestBoardConfigUnmarshalObjectMapping(t *testing.T) {
	jsonData := []byte(`{"board":{"stateMapping":{"TeamA":{"Queued":"Todo"}}}}`)
	cfg := DefaultConfig()
	if err := json.Unmarshal(jsonData, cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Board.StateMappingMode != "custom" {
		t.Fatalf("expected stateMappingMode custom, got %q", cfg.Board.StateMappingMode)
	}
	if cfg.Board.StateMapping["TeamA"]["Queued"] != "Todo" {
		t.Fatalf("expected mapping value")
	}
}

func TestBoardConfigUnmarshalWIPLimits(t *testing.T) {
	jsonData := []byte(`{"board":{"columns":["Todo","Done"],"wipLimits":{"Todo":3}}}`)
	cfg := DefaultConfig()
	if err := json.Unmarshal(jsonData, cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Board.WIPLimits["Todo"] != 3 {
		t.Fatalf("expected wip limit to be parsed")
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	if len(cfg.Board.Columns) == 0 {
		t.Fatalf("expected default columns")
	}
	if cfg.Scope.UpdatedWithinDays == 0 {
		t.Fatalf("expected default updatedWithinDays")
	}
}
