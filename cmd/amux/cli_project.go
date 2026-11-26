package main

// The project-facing CLI surface: `amux project ls` (the registry with the
// authoritative names) and `amux project rename` (rename + worktree
// migration). Unlike the agent/status verbs these bootstrap the registry and
// workspace metadata — but still no TTY, TUI, or logging/config
// initialization, so they run from scripts and panes alike.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/validation"
)

// HandleProjectCLI dispatches the project subcommands. It returns handled=false
// for any argv it does not own, so it composes with the other pre-TTY
// dispatchers in main.
func HandleProjectCLI(args []string) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}
	if args[0] != "project" {
		return false, 0
	}
	if len(args[1:]) == 0 {
		fmt.Fprintln(os.Stderr, "usage: amux project ls | amux project rename <path> <new-name>")
		return true, 2
	}
	switch args[1] {
	case "ls":
		return true, runProjectListCommand(args[2:])
	case "rename":
		return true, runProjectRenameCommand(args[2:])
	default:
		fmt.Fprintf(os.Stderr, "amux project: unknown subcommand %q (only 'ls' and 'rename')\n", args[1])
		return true, 2
	}
}

// projectListRow is one registered project as `amux project ls` reports it.
// The JSON field names are a seam for scripts — change them only with docs
// and release notes.
type projectListRow struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func runProjectListCommand(args []string) int {
	fs := flag.NewFlagSet("amux project ls", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print one JSON object per project")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "amux project ls: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	entries, err := loadRegistryEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux project: %v\n", err)
		if *asJSON {
			// The shared verb contract: --json failures also write the
			// machine envelope to stderr so a script never parses prose.
			printJSONErrorEnvelope("error", err.Error())
		}
		return 1
	}
	rows := make([]projectListRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, projectListRow{Name: entry.Name, Path: entry.Path})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, row := range rows {
			if err := enc.Encode(row); err != nil {
				fmt.Fprintf(os.Stderr, "amux project ls: %v\n", err)
				return 1
			}
		}
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\n", row.Name, row.Path)
	}
	w.Flush()
	return 0
}

// runProjectRenameCommand implements `amux project rename <path> <new-name>`:
// the registry rename lands first (safe to interrupt), then managed
// worktrees migrate with git worktree move and their records rebind in place
// — session names, tags, and port reservations keep their IDs. Shelved
// records rebind metadata-only.
//
// Exit codes follow the shared verb contract: 2 usage, 3 the project path is
// not registered, 1 any other failure (rename refused, move failed), 0
// success — including a partial migration, whose counts the output reports.
func runProjectRenameCommand(args []string) int {
	fs := flag.NewFlagSet("amux project rename", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: amux project rename <path> <new-name>")
		return 2
	}
	path, newName := fs.Arg(0), fs.Arg(1)

	expanded, err := config.ExpandHomePath(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux project rename: %v\n", err)
		return 2
	}
	newName = strings.TrimSpace(newName)
	if err := validation.ValidateProjectName(newName); err != nil {
		fmt.Fprintf(os.Stderr, "amux project rename: %v\n", err)
		return 2
	}
	if !git.IsGitRepository(expanded) {
		fmt.Fprintf(os.Stderr, "amux project rename: %s is not a git repository\n", expanded)
		return 3
	}

	svc, entries, code := loadProjectService()
	if code != 0 {
		return code
	}
	var project *data.Project
	for _, entry := range entries {
		if data.SamePath(entry.Path, expanded) {
			project = data.NewProjectNamed(entry.Name, entry.Path)
			break
		}
	}
	if project == nil {
		fmt.Fprintf(os.Stderr, "amux project rename: %s is not a registered project (see `amux project ls`)\n", expanded)
		return 3
	}

	var cmd tea.Cmd = svc.RenameProject(project, newName)
	msg := cmd()
	switch result := msg.(type) {
	case messages.Error:
		fmt.Fprintf(os.Stderr, "amux project rename: %v\n", result.Err)
		return 1
	case messages.ProjectRenamed:
		fmt.Printf("renamed %s (%s) to %s: moved=%d skipped=%d failed=%d\n",
			result.Path, result.OldName, result.NewName, result.Moved, result.Skipped, result.Failed)
		if result.Err != nil {
			// Err is set for per-worktree failures and for whole-migration
			// failures that carry no counts (container creation, discovery)
			// — Failed can be 0 with nothing moved, so the error, not the
			// count, is the failure signal.
			fmt.Fprintf(os.Stderr, "amux project rename: migration incomplete (moved=%d skipped=%d failed=%d): %v\n",
				result.Moved, result.Skipped, result.Failed, result.Err)
			return 1
		}
		if result.Skipped > 0 {
			fmt.Fprintln(os.Stderr, "amux project rename: close the running scripts in skipped worktrees and re-run to move them")
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "amux project rename: unexpected result %T\n", msg)
		return 1
	}
}

// loadRegistryEntries returns the registry entries, or the failure for the
// caller to report (the --json callers add the machine error envelope).
func loadRegistryEntries() ([]data.RegistryEntry, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return data.NewRegistry(paths.RegistryPath).Entries()
}

// loadProjectService builds the headless workspace service over the default
// paths: the real registry, the real metadata store, the real git operations,
// no script runner (the CLI hosts none — close hosted agents before renaming
// out from under them).
func loadProjectService() (*workspacesvc.Service, []data.RegistryEntry, int) {
	entries, err := loadRegistryEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux project: %v\n", err)
		return nil, nil, 1
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux project: %v\n", err)
		return nil, nil, 1
	}
	svc := workspacesvc.New(
		data.NewRegistry(paths.RegistryPath),
		data.NewWorkspaceStore(paths.MetadataRoot),
		nil,
		paths.WorkspacesRoot,
	)
	return svc, entries, 0
}
