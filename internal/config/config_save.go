package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
)

// readConfigForUpdate reads the existing config document for a
// read-modify-write save. It is deliberately STRICT where the startup loader
// is tolerant: a save that cannot see the real document must fail rather than
// rewrite the file from a partial view, because doing so would silently drop
// sections the save caller does not own.
//
// Boundary rules: a missing file and a blank file both read as an empty
// object; any other read error is propagated (wrapped with the path); a
// nonblank read must decode into a JSON object — malformed JSON, a top-level
// `null`, and non-object roots are all refused so no caller mutates a payload
// it cannot preserve.
//
// The reader is a parameter so tests inject failures without a package-global
// seam; production callers pass readConfigPath.
func readConfigForUpdate(path string, read func(string) ([]byte, error)) (map[string]any, error) {
	existing, err := read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("refusing to overwrite unreadable config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(existing, &payload); err != nil {
		return nil, fmt.Errorf("refusing to overwrite malformed config %s: %w", path, err)
	}
	if payload == nil {
		// JSON `null` decodes cleanly into a nil map — mutating it would panic,
		// and overwriting it would discard whatever else the file held.
		return nil, fmt.Errorf("refusing to overwrite non-object config %s", path)
	}
	return payload, nil
}
