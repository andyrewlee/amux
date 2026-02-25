package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SandboxAction describes what a sandbox rule does.
type SandboxAction string

const (
	SandboxAllowRead  SandboxAction = "allow-read"
	SandboxAllowWrite SandboxAction = "allow-write"
	SandboxDenyRead   SandboxAction = "deny-read"
)

// SandboxPathType describes how the path should be matched in SBPL.
type SandboxPathType string

const (
	SandboxSubpath SandboxPathType = "subpath"
	SandboxLiteral SandboxPathType = "literal"
	SandboxRegex   SandboxPathType = "regex"
)

// SandboxRule is a single configurable sandbox path rule.
type SandboxRule struct {
	Path     string          `json:"path"`
	Action   SandboxAction   `json:"action"`
	PathType SandboxPathType `json:"path_type"`
	Locked   bool            `json:"locked"`
	Comment  string          `json:"comment"`
}

// SandboxRules holds the full set of sandbox path rules.
type SandboxRules struct {
	Rules []SandboxRule `json:"rules"`
}

// DefaultSandboxRules returns the built-in default rules that mirror the
// previously-hardcoded paths in GenerateSBPL.
func DefaultSandboxRules() *SandboxRules {
	return &SandboxRules{
		Rules: []SandboxRule{
			{Path: "~/.ssh", Action: SandboxDenyRead, PathType: SandboxSubpath, Comment: "SSH keys"},
			{Path: "~/.gnupg", Action: SandboxDenyRead, PathType: SandboxSubpath, Comment: "GPG keys"},
			{Path: "~/.aws", Action: SandboxDenyRead, PathType: SandboxSubpath, Comment: "AWS credentials"},
			{Path: "~/.docker", Action: SandboxDenyRead, PathType: SandboxSubpath, Comment: "Docker config"},
			{Path: "~/.kube", Action: SandboxDenyRead, PathType: SandboxSubpath, Comment: "Kubernetes config"},
			{Path: "~/.ssh/known_hosts", Action: SandboxAllowRead, PathType: SandboxLiteral, Comment: "SSH host verification"},
			{Path: "~/.local/state/claude", Action: SandboxAllowWrite, PathType: SandboxSubpath, Comment: "Claude version locks"},
			{Path: "~/.npm", Action: SandboxAllowWrite, PathType: SandboxSubpath, Comment: "npm cache (MCP servers)"},
		},
	}
}

// LoadSandboxRules reads sandbox rules from path. If the file does not exist,
// it writes the defaults and returns them.
func LoadSandboxRules(path string) (*SandboxRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaults := DefaultSandboxRules()
			if writeErr := SaveSandboxRules(path, defaults); writeErr != nil {
				return defaults, nil // return defaults even if write fails
			}
			return defaults, nil
		}
		return nil, err
	}
	var rules SandboxRules
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

// SaveSandboxRules writes sandbox rules to path atomically using a tmp+rename.
func SaveSandboxRules(path string, rules *SandboxRules) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ExpandSandboxPath replaces a leading ~ with $HOME. Regex paths (starting
// with ^) are returned as-is.
func ExpandSandboxPath(path string) string {
	if strings.HasPrefix(path, "^") {
		return path // regex pattern, passthrough
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}
