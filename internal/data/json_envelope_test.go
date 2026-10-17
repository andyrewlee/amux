package data

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestEnvelope builds a projectEnvelope over a synthetic record type —
// the mechanics contract a future same-shape store would consume by
// choosing parameters, not by forking code.
func newTestEnvelope(t *testing.T) *projectEnvelope[string] {
	t.Helper()
	type syntheticFile struct {
		Version int               `json:"version"`
		Records map[string]string `json:"records"`
	}
	return &projectEnvelope[string]{
		path: filepath.Join(t.TempDir(), "synthetic.json"),
		decodeEnvelope: func(raw []byte) (map[string]string, error) {
			var file syntheticFile
			if err := json.Unmarshal(raw, &file); err != nil {
				return nil, err
			}
			if file.Version > 1 {
				return nil, errors.New("synthetic: newer schema")
			}
			if file.Records == nil {
				return map[string]string{}, nil
			}
			return file.Records, nil
		},
		encodeEnvelope: func(all map[string]string) any {
			return syntheticFile{Version: 1, Records: all}
		},
		emptyEntry: func(v string) bool { return v == "" },
	}
}

func TestProjectEnvelope_RoundTripAndEmptyDelete(t *testing.T) {
	s := newTestEnvelope(t)
	repo := t.TempDir()
	if err := s.set(repo, "value"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.forRepo(repo, "zero"); got != "value" {
		t.Fatalf("forRepo = %q, want value", got)
	}
	if err := s.set(repo, ""); err != nil {
		t.Fatalf("set empty: %v", err)
	}
	// Absent key yields V's zero — the `zero` argument only covers the
	// load-failure and invalid-key paths.
	if got := s.forRepo(repo, "zero"); got != "" {
		t.Fatalf("forRepo after delete = %q, want \"\"", got)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if strings.Contains(string(raw), filepath.Base(NormalizePath(repo))) {
		t.Fatalf("empty record persisted in file: %s", raw)
	}
}

func TestProjectEnvelope_CorruptWritePreservesAndRefuses(t *testing.T) {
	s := newTestEnvelope(t)
	if err := os.WriteFile(s.Path(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	// A decode failure on read yields the caller's zero — never blocks.
	if got := s.forRepo(repo, "zero"); got != "zero" {
		t.Fatalf("forRepo on corrupt file = %q, want zero", got)
	}
	err := s.set(repo, "value")
	var csErr *CorruptStoreError
	if !errors.As(err, &csErr) {
		t.Fatalf("set err = %v, want CorruptStoreError", err)
	}
	if csErr.Preserved == "" {
		t.Fatal("corrupt primary not preserved")
	}
	if _, err := os.Stat(csErr.Preserved); err != nil {
		t.Fatalf("preserved backup missing: %v", err)
	}
	// The refused write must not have created a fresh primary.
	if _, err := os.Stat(s.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("primary exists after refused write: %v", err)
	}
}

func TestProjectEnvelope_SchemaRefusalIsNotCorruption(t *testing.T) {
	s := newTestEnvelope(t)
	if err := os.WriteFile(s.Path(), []byte(`{"version":99,"records":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	err := s.set(repo, "value")
	var csErr *CorruptStoreError
	if errors.As(err, &csErr) {
		t.Fatalf("newer-schema refusal misclassified as corruption: %v", err)
	}
	if err == nil {
		t.Fatal("set on newer-schema file returned nil")
	}
	// The newer file must remain untouched — no .bak, same bytes.
	if _, err := os.Stat(s.Path() + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("newer-schema file preserved as .bak: %v", err)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil || string(raw) != `{"version":99,"records":{}}` {
		t.Fatalf("newer-schema file modified: %q err=%v", raw, err)
	}
}

func TestProjectEnvelope_RejectsEmptyRepoPath(t *testing.T) {
	s := newTestEnvelope(t)
	if err := s.set("", "v"); err == nil {
		t.Fatal("set with empty repo path returned nil")
	}
}
