package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/git"
)

func canonicalizeProjectPathNoSymlinks(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// lenientCanonicalizePath resolves a path to an absolute, cleaned form without
// requiring the path to exist on disk. It tries EvalSymlinks on the full path
// first. If that fails (e.g. the directory was deleted), it resolves the
// parent directory's symlinks and appends the base name, so that platform
// symlinks like /tmp → /private/tmp are still resolved correctly.
func lenientCanonicalizePath(path string) string {
	if canon, err := canonicalizeProjectPath(path); err == nil {
		return canon
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	// Try resolving the parent; the leaf may not exist but the parent might.
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(resolvedDir, base)
	}
	return abs
}

// --- project routing ---

func routeProject(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return routeSubcommand(w, wErr, gf, args, version, "project", []subcommand{
		{names: []string{"list", "ls"}, handler: cmdProjectList},
		{names: []string{"add"}, handler: cmdProjectAdd},
		{names: []string{"remove", "rm"}, handler: cmdProjectRemove},
	})
}

// --- project list ---

type projectEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func cmdProjectList(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux project list [--json]"
	if len(args) > 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("unexpected arguments"))
	}

	svc, code := initServicesOrFail(w, wErr, gf, version)
	if code >= 0 {
		return code
	}

	paths, err := svc.Registry.Projects()
	if err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "list_failed", err, nil,
			"failed to list projects: %v", err)
	}

	entries := make([]projectEntry, len(paths))
	for i, p := range paths {
		entries[i] = projectEntry{
			Name: filepath.Base(p),
			Path: p,
		}
	}

	if gf.JSON {
		PrintJSON(w, entries, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		if len(entries) == 0 {
			fmt.Fprintln(w, "No projects registered.")
			return
		}
		for _, e := range entries {
			fmt.Fprintf(w, "  %s\t%s\n", e.Name, e.Path)
		}
	})
	return ExitOK
}

// --- project add ---

func cmdProjectAdd(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux project add <path> [--json]"
	fs := newFlagSet("project add")
	path, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if path == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	projectPath, err := canonicalizeProjectPath(path)
	if err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitUsage, "invalid_project_path", err, map[string]any{"path": path},
			"invalid path: %v", err)
	}

	if !git.IsGitRepository(projectPath) {
		return returnOperationError(w, wErr, gf, version,
			ExitUsage, "not_git_repo", fmt.Errorf("%s is not a git repository", projectPath),
			map[string]any{"path": projectPath},
			"%s is not a git repository", projectPath)
	}

	svc, code := initServicesOrFail(w, wErr, gf, version)
	if code >= 0 {
		return code
	}

	if err := svc.Registry.AddProject(projectPath); err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "add_failed", err, map[string]any{"path": projectPath},
			"failed to add project: %v", err)
	}

	entry := projectEntry{
		Name: filepath.Base(projectPath),
		Path: projectPath,
	}

	if gf.JSON {
		PrintJSON(w, entry, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Added project %s (%s)\n", entry.Name, entry.Path)
	})
	return ExitOK
}

// --- project remove ---

func cmdProjectRemove(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux project remove <path> [--json]"
	fs := newFlagSet("project remove")
	path, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if path == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	// Use registry-compatible canonicalization so removal works for paths stored
	// without symlink resolution, and also try lenient canonicalization for
	// deleted/moved paths that still need cleanup.
	projectPath := lenientCanonicalizePath(path)
	projectPathNoResolve := canonicalizeProjectPathNoSymlinks(path)

	svc, code := initServicesOrFail(w, wErr, gf, version)
	if code >= 0 {
		return code
	}

	for candidate := range map[string]struct{}{
		projectPath:          {},
		projectPathNoResolve: {},
	} {
		if candidate == "" {
			continue
		}
		if err := svc.Registry.RemoveProject(candidate); err != nil {
			return returnOperationError(w, wErr, gf, version,
				ExitInternalError, "remove_failed", err, map[string]any{"path": candidate},
				"failed to remove project: %v", err)
		}
	}

	displayPath := projectPathNoResolve
	if displayPath == "" {
		displayPath = projectPath
	}

	entry := projectEntry{
		Name: filepath.Base(displayPath),
		Path: displayPath,
	}

	if gf.JSON {
		PrintJSON(w, entry, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Removed project %s (%s)\n", entry.Name, entry.Path)
	})
	return ExitOK
}
