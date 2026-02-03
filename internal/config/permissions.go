package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GlobalPermissions stores the global allow/deny permission lists.
type GlobalPermissions struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// legacyPermPattern matches legacy permission syntax like "Bash(cmd:*)" or "Read(path:*)"
var legacyPermPattern = regexp.MustCompile(`^([A-Za-z_]+)\((.+):\*\)$`)

// NormalizePermission converts legacy permission format to the new format.
// Legacy: "Bash(ls:*)" -> New: "Bash(ls *)"
// The :* suffix is deprecated and equivalent to space + *.
func NormalizePermission(perm string) string {
	perm = strings.TrimSpace(perm)
	if matches := legacyPermPattern.FindStringSubmatch(perm); matches != nil {
		// matches[1] = tool name (e.g., "Bash")
		// matches[2] = specifier without :* (e.g., "ls")
		return matches[1] + "(" + matches[2] + " *)"
	}
	return perm
}

// NormalizePermissions normalizes a slice of permissions.
func NormalizePermissions(perms []string) []string {
	if perms == nil {
		return nil
	}
	result := make([]string, len(perms))
	for i, p := range perms {
		result[i] = NormalizePermission(p)
	}
	return result
}

// LoadGlobalPermissions reads the global permissions from disk.
// Returns an empty permissions struct if the file does not exist.
// Permissions are normalized to the new format on load.
func LoadGlobalPermissions(path string) (*GlobalPermissions, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobalPermissions{}, nil
		}
		return nil, err
	}
	var perms GlobalPermissions
	if err := json.Unmarshal(data, &perms); err != nil {
		return nil, err
	}
	// Normalize and deduplicate on load to handle legacy formats
	perms.Allow = dedupe(perms.Allow)
	perms.Deny = dedupe(perms.Deny)
	return &perms, nil
}

// SaveGlobalPermissions writes the global permissions to disk atomically.
func SaveGlobalPermissions(path string, perms *GlobalPermissions) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// Deduplicate and ensure non-nil slices for proper JSON marshaling
	toSave := &GlobalPermissions{
		Allow: dedupe(perms.Allow),
		Deny:  dedupe(perms.Deny),
	}
	data, err := json.MarshalIndent(toSave, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// dedupe removes duplicate entries from a slice while preserving order.
// It also normalizes legacy permission formats.
func dedupe(list []string) []string {
	if list == nil {
		return []string{}
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(list))
	for _, s := range list {
		normalized := NormalizePermission(s)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	return result
}

// AddAllow adds a permission to the allow list if not already present.
// The permission is normalized to the new format before adding.
func (p *GlobalPermissions) AddAllow(perm string) bool {
	normalized := NormalizePermission(perm)
	if containsNormalized(p.Allow, normalized) {
		return false
	}
	p.Allow = append(p.Allow, normalized)
	return true
}

// AddDeny adds a permission to the deny list if not already present.
// The permission is normalized to the new format before adding.
func (p *GlobalPermissions) AddDeny(perm string) bool {
	normalized := NormalizePermission(perm)
	if containsNormalized(p.Deny, normalized) {
		return false
	}
	p.Deny = append(p.Deny, normalized)
	return true
}

// RemoveAllow removes a permission from the allow list.
// Comparison is done after normalizing both values.
func (p *GlobalPermissions) RemoveAllow(perm string) bool {
	normalized := NormalizePermission(perm)
	for i, v := range p.Allow {
		if NormalizePermission(v) == normalized {
			p.Allow = append(p.Allow[:i], p.Allow[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveDeny removes a permission from the deny list.
// Comparison is done after normalizing both values.
func (p *GlobalPermissions) RemoveDeny(perm string) bool {
	normalized := NormalizePermission(perm)
	for i, v := range p.Deny {
		if NormalizePermission(v) == normalized {
			p.Deny = append(p.Deny[:i], p.Deny[i+1:]...)
			return true
		}
	}
	return false
}

// MoveToAllow moves a permission from the deny list to the allow list.
func (p *GlobalPermissions) MoveToAllow(perm string) {
	p.RemoveDeny(perm)
	p.AddAllow(perm)
}

// MoveToDeny moves a permission from the allow list to the deny list.
func (p *GlobalPermissions) MoveToDeny(perm string) {
	p.RemoveAllow(perm)
	p.AddDeny(perm)
}

// DiffPermissions returns entries present in incoming but not in existing.
// Permissions are compared after normalization.
func DiffPermissions(existing, incoming []string) []string {
	set := make(map[string]bool, len(existing))
	for _, v := range existing {
		set[NormalizePermission(v)] = true
	}
	var diff []string
	for _, v := range incoming {
		normalized := NormalizePermission(v)
		if !set[normalized] {
			set[normalized] = true // prevent duplicates in diff
			diff = append(diff, normalized)
		}
	}
	return diff
}

// containsNormalized checks if a normalized permission exists in the list.
// The list entries are normalized before comparison.
func containsNormalized(list []string, normalizedItem string) bool {
	for _, v := range list {
		if NormalizePermission(v) == normalizedItem {
			return true
		}
	}
	return false
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}
