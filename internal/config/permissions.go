package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// GlobalPermissions stores the global allow/deny permission lists.
type GlobalPermissions struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// LoadGlobalPermissions reads the global permissions from disk.
// Returns an empty permissions struct if the file does not exist.
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
	// Deduplicate on load
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
func dedupe(list []string) []string {
	if list == nil {
		return []string{}
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(list))
	for _, s := range list {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}
	return result
}

// AddAllow adds a permission to the allow list if not already present.
func (p *GlobalPermissions) AddAllow(perm string) bool {
	perm = strings.TrimSpace(perm)
	if contains(p.Allow, perm) {
		return false
	}
	p.Allow = append(p.Allow, perm)
	return true
}

// AddDeny adds a permission to the deny list if not already present.
func (p *GlobalPermissions) AddDeny(perm string) bool {
	perm = strings.TrimSpace(perm)
	if contains(p.Deny, perm) {
		return false
	}
	p.Deny = append(p.Deny, perm)
	return true
}

// RemoveAllow removes a permission from the allow list.
func (p *GlobalPermissions) RemoveAllow(perm string) bool {
	perm = strings.TrimSpace(perm)
	for i, v := range p.Allow {
		if strings.TrimSpace(v) == perm {
			p.Allow = append(p.Allow[:i], p.Allow[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveDeny removes a permission from the deny list.
func (p *GlobalPermissions) RemoveDeny(perm string) bool {
	perm = strings.TrimSpace(perm)
	for i, v := range p.Deny {
		if strings.TrimSpace(v) == perm {
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
func DiffPermissions(existing, incoming []string) []string {
	set := make(map[string]bool, len(existing))
	for _, v := range existing {
		set[strings.TrimSpace(v)] = true
	}
	var diff []string
	for _, v := range incoming {
		trimmed := strings.TrimSpace(v)
		if !set[trimmed] {
			set[trimmed] = true // prevent duplicates in diff
			diff = append(diff, trimmed)
		}
	}
	return diff
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if strings.TrimSpace(v) == item {
			return true
		}
	}
	return false
}
