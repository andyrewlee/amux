package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// workspaceJSON is used for loading old-format metadata files during migration
type workspaceJSON struct {
	Name           string            `json:"name"`
	Branch         string            `json:"branch"`
	Repo           string            `json:"repo"`
	Base           string            `json:"base"`
	Root           string            `json:"root"`
	Created        json.RawMessage   `json:"created"` // Can be time.Time or string
	Archived       bool              `json:"archived"`
	ArchivedAt     json.RawMessage   `json:"archived_at,omitempty"`
	Assistant      string            `json:"assistant"`
	Runtime        string            `json:"runtime"`
	Scripts        ScriptsConfig     `json:"scripts"`
	ScriptMode     string            `json:"script_mode"`
	Env            map[string]string `json:"env"`
	OpenTabs       []TabInfo         `json:"open_tabs,omitempty"`
	ActiveTabIndex int               `json:"active_tab_index"`
}

// parseCreated parses a created timestamp from either time.Time format or string format
func parseCreated(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}

	// Try parsing as time.Time first (JSON format)
	var t time.Time
	if err := json.Unmarshal(raw, &t); err == nil && !t.IsZero() {
		return t
	}

	// Try parsing as string (RFC3339 format from old metadata)
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return parsed
		}
	}

	return time.Time{}
}

// validateWorkspaceID rejects IDs containing "..", "/", or "\" to prevent path
// traversal attacks — workspace IDs are used to construct filesystem paths.
// This is intentionally stricter than the WorkspaceID type itself.
func validateWorkspaceID(id WorkspaceID) error {
	value := strings.TrimSpace(string(id))
	if value == "" {
		return errors.New("workspace id is required")
	}
	if strings.Contains(value, "..") || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("invalid workspace id %q", id)
	}
	return nil
}

func validateWorkspaceForSave(ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	repo := NormalizePath(strings.TrimSpace(ws.Repo))
	if repo == "" {
		return errors.New("workspace repo is required")
	}
	root := NormalizePath(strings.TrimSpace(ws.Root))
	if root == "" {
		return errors.New("workspace root is required")
	}
	return nil
}
