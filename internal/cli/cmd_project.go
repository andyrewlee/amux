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
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", "Usage: amux project <list|add|remove> [flags]", nil, version)
		} else {
			fmt.Fprintln(wErr, "Usage: amux project <list|add|remove> [flags]")
		}
		return ExitUsage
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "list", "ls":
		return cmdProjectList(w, wErr, gf, subArgs, version)
	case "add":
		return cmdProjectAdd(w, wErr, gf, subArgs, version)
	case "remove", "rm":
		return cmdProjectRemove(w, wErr, gf, subArgs, version)
	default:
		if gf.JSON {
			ReturnError(w, "unknown_command", "Unknown project subcommand: "+sub, nil, version)
		} else {
			fmt.Fprintf(wErr, "Unknown project subcommand: %s\n", sub)
		}
		return ExitUsage
	}
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

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	paths, err := svc.Registry.Projects()
	if err != nil {
		if gf.JSON {
			ReturnError(w, "list_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to list projects: %v", err)
		}
		return ExitInternalError
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
		if gf.JSON {
			ReturnError(w, "invalid_project_path", err.Error(), map[string]any{"path": path}, version)
		} else {
			Errorf(wErr, "invalid path: %v", err)
		}
		return ExitUsage
	}

	if !git.IsGitRepository(projectPath) {
		if gf.JSON {
			ReturnError(w, "not_git_repo", projectPath+" is not a git repository", map[string]any{"path": projectPath}, version)
		} else {
			Errorf(wErr, "%s is not a git repository", projectPath)
		}
		return ExitUsage
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	if err := svc.Registry.AddProject(projectPath); err != nil {
		if gf.JSON {
			ReturnError(w, "add_failed", err.Error(), map[string]any{"path": projectPath}, version)
		} else {
			Errorf(wErr, "failed to add project: %v", err)
		}
		return ExitInternalError
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

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	for candidate := range map[string]struct{}{
		projectPath:          {},
		projectPathNoResolve: {},
	} {
		if candidate == "" {
			continue
		}
		if err := svc.Registry.RemoveProject(candidate); err != nil {
			if gf.JSON {
				ReturnError(w, "remove_failed", err.Error(), map[string]any{"path": candidate}, version)
			} else {
				Errorf(wErr, "failed to remove project: %v", err)
			}
			return ExitInternalError
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
