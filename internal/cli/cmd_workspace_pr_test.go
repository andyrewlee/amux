package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakePRStore struct {
	Next  int                 `json:"next"`
	PRs   map[string]ghPRView `json:"prs"`
	Calls []fakePRCall        `json:"calls,omitempty"`
}

type fakePRCall struct {
	Subcommand string `json:"subcommand"`
	Repo       string `json:"repo,omitempty"`
	Head       string `json:"head,omitempty"`
}

func TestWorkspacePRHelperProcess(t *testing.T) {
	if os.Getenv("AMUX_TEST_PR_HELPER") != "1" {
		return
	}

	args := os.Args
	dash := -1
	for i, arg := range args {
		if arg == "--" {
			dash = i
			break
		}
	}
	if dash < 0 || dash+1 >= len(args) {
		fmt.Fprint(os.Stderr, "missing helper args")
		os.Exit(2)
	}
	cmdArgs := args[dash+1:]
	if len(cmdArgs) < 3 || cmdArgs[0] != "gh" || cmdArgs[1] != "pr" {
		fmt.Fprintf(os.Stderr, "unsupported helper args: %v", cmdArgs)
		os.Exit(2)
	}

	storePath := os.Getenv("AMUX_TEST_PR_STORE")
	if storePath == "" {
		fmt.Fprint(os.Stderr, "missing AMUX_TEST_PR_STORE")
		os.Exit(2)
	}
	store := loadFakePRStore(t, storePath)
	store.Calls = append(store.Calls, fakePRCall{
		Subcommand: cmdArgs[2],
		Repo:       helperFlagValue(cmdArgs[2:], "--repo"),
		Head:       helperHeadArg(cmdArgs),
	})
	saveFakePRStore(t, storePath, store)

	switch cmdArgs[2] {
	case "view":
		target := cmdArgs[3]
		pr, ok := helperLookupPRForView(store.PRs, target)
		if !ok {
			fmt.Fprint(os.Stderr, "no pull request found")
			os.Exit(1)
		}
		encoded, _ := json.Marshal(pr)
		fmt.Print(string(encoded))
	case "list":
		head := helperFlagValue(cmdArgs[3:], "--head")
		prs := helperListPRsForHead(store.PRs, head)
		encoded, _ := json.Marshal(prs)
		fmt.Print(string(encoded))
	case "create":
		head := helperFlagValue(cmdArgs[3:], "--head")
		base := helperFlagValue(cmdArgs[3:], "--base")
		title := helperFlagValue(cmdArgs[3:], "--title")
		body := helperFlagValue(cmdArgs[3:], "--body")
		if title == "" {
			title = head
		}
		if _, exists := store.PRs[head]; exists {
			fmt.Fprint(os.Stderr, "pull request already exists")
			os.Exit(1)
		}
		store.Next++
		headOwner, headBranch := helperSplitHeadSelector(head)
		pr := ghPRView{
			Number:              store.Next,
			URL:                 fmt.Sprintf("https://example.com/pr/%d", store.Next),
			Title:               title,
			Body:                body,
			HeadRefName:         headBranch,
			HeadRepositoryOwner: ghPRRepositoryOwner{Login: headOwner},
			BaseRefName:         base,
			IsDraft:             helperHasFlag(cmdArgs[3:], "--draft"),
			State:               "OPEN",
		}
		store.PRs[head] = pr
		saveFakePRStore(t, storePath, store)
		fmt.Print(pr.URL)
	case "edit":
		numberRaw := cmdArgs[3]
		number, err := strconv.Atoi(numberRaw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid PR number %q", numberRaw)
			os.Exit(2)
		}
		var head string
		var pr ghPRView
		found := false
		for branch, candidate := range store.PRs {
			if candidate.Number == number {
				head = branch
				pr = candidate
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "pull request %d not found", number)
			os.Exit(1)
		}
		if base := helperFlagValue(cmdArgs[4:], "--base"); base != "" {
			pr.BaseRefName = base
		}
		if title := helperFlagValue(cmdArgs[4:], "--title"); title != "" {
			pr.Title = title
		}
		if body := helperFlagValue(cmdArgs[4:], "--body"); body != "" {
			pr.Body = body
		}
		store.PRs[head] = pr
		saveFakePRStore(t, storePath, store)
	default:
		fmt.Fprintf(os.Stderr, "unsupported gh pr subcommand: %s", cmdArgs[2])
		os.Exit(2)
	}
	os.Exit(0)
}

func TestCmdWorkspacePRRecursiveAndRetargetsExistingPR(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, _ := initRegisteredRepoWithOrigin(t, home)

	root := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(root.Root, "root.txt"), []byte("root-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(root-1) error = %v", err)
	}
	runGit(t, root.Root, "add", "root.txt")
	runGit(t, root.Root, "commit", "-m", "root 1")

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", root.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	storePath, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{root.ID, "--recursive", "--draft"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR(create) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v\nraw: %s", err, out.String())
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	prs, ok := payload["pull_requests"].([]any)
	if !ok || len(prs) != 2 {
		t.Fatalf("expected 2 pull requests, got %#v", payload["pull_requests"])
	}
	rootPR, ok := prs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected root PR object, got %T", prs[0])
	}
	childPR, ok := prs[1].(map[string]any)
	if !ok {
		t.Fatalf("expected child PR object, got %T", prs[1])
	}
	if got := stringValue(rootPR["action"]); got != "created" {
		t.Fatalf("root PR action = %q, want %q", got, "created")
	}
	if got := stringValue(rootPR["base"]); got != "main" {
		t.Fatalf("root PR base = %q, want %q", got, "main")
	}
	if got := stringValue(childPR["action"]); got != "created" {
		t.Fatalf("child PR action = %q, want %q", got, "created")
	}
	if got := stringValue(childPR["base"]); got != root.Branch {
		t.Fatalf("child PR base = %q, want %q", got, root.Branch)
	}

	alt := createWorkspaceForTest(
		t,
		"test-v1",
		"alt", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(alt.Root, "alt.txt"), []byte("alt-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(alt-1) error = %v", err)
	}
	runGit(t, alt.Root, "add", "alt.txt")
	runGit(t, alt.Root, "commit", "-m", "alt 1")

	out.Reset()
	errOut.Reset()
	code = cmdWorkspaceReparent(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID, "--parent", alt.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceReparent() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	out.Reset()
	errOut.Reset()
	code = cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR(update) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}
	env = Envelope{}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(update) error = %v\nraw: %s", err, out.String())
	}
	payload, ok = env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	prs, ok = payload["pull_requests"].([]any)
	if !ok || len(prs) != 1 {
		t.Fatalf("expected 1 pull request, got %#v", payload["pull_requests"])
	}
	updatedPR, ok := prs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected updated PR object, got %T", prs[0])
	}
	if got := stringValue(updatedPR["action"]); got != "updated" {
		t.Fatalf("updated PR action = %q, want %q", got, "updated")
	}
	if got := stringValue(updatedPR["base"]); got != alt.Branch {
		t.Fatalf("updated PR base = %q, want %q", got, alt.Branch)
	}

	store := loadFakePRStore(t, storePath)
	if len(store.Calls) == 0 {
		t.Fatal("expected gh calls to be recorded")
	}
	for _, call := range store.Calls {
		if call.Repo != "amux-test/origin" {
			t.Fatalf("gh call repo = %q, want %q", call.Repo, "amux-test/origin")
		}
	}
}

func TestCmdWorkspacePRRecursiveSkipsEmptyAncestors(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, remoteDir := initRegisteredRepoWithOrigin(t, home)

	root := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", root.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	storePath, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{root.ID, "--recursive"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	prs, ok := payload["pull_requests"].([]any)
	if !ok || len(prs) != 1 {
		t.Fatalf("expected 1 pull request, got %#v", payload["pull_requests"])
	}
	skipped, ok := payload["skipped"].([]any)
	if !ok || len(skipped) != 1 {
		t.Fatalf("expected 1 skipped workspace, got %#v", payload["skipped"])
	}
	skippedRoot, ok := skipped[0].(map[string]any)
	if !ok {
		t.Fatalf("expected skipped workspace object, got %T", skipped[0])
	}
	if got := stringValue(skippedRoot["workspace_name"]); got != root.Name {
		t.Fatalf("skipped workspace_name = %q, want %q", got, root.Name)
	}
	if got := stringValue(skippedRoot["action"]); got != "skipped" {
		t.Fatalf("skipped action = %q, want %q", got, "skipped")
	}

	store := loadFakePRStore(t, storePath)
	if _, ok := store.PRs[child.Branch]; !ok {
		t.Fatalf("expected child PR for branch %q, store = %#v", child.Branch, store.PRs)
	}
	if _, ok := store.PRs[root.Branch]; ok {
		t.Fatalf("unexpected PR created for empty root branch %q", root.Branch)
	}
	if !remoteHasBranch(t, remoteDir, root.Branch) {
		t.Fatalf("expected skipped ancestor branch %q to be pushed for descendant PR base", root.Branch)
	}
}

func stubGHCLIForTest(t *testing.T) (string, func()) {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "prs.json")
	saveFakePRStore(t, storePath, fakePRStore{PRs: map[string]ghPRView{}})

	origLookPath := cliLookPath
	origExec := cliExecCommandContext
	cliLookPath = func(file string) (string, error) {
		if file == "gh" {
			return "/test/bin/gh", nil
		}
		return origLookPath(file)
	}
	cliExecCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name != "gh" {
			return origExec(ctx, name, args...)
		}
		cmdArgs := append([]string{"-test.run=TestWorkspacePRHelperProcess", "--", name}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
		cmd.Env = append(os.Environ(),
			"AMUX_TEST_PR_HELPER=1",
			"AMUX_TEST_PR_STORE="+storePath,
		)
		return cmd
	}
	return storePath, func() {
		cliLookPath = origLookPath
		cliExecCommandContext = origExec
	}
}

func loadFakePRStore(t *testing.T, path string) fakePRStore {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fakePRStore{PRs: map[string]ghPRView{}}
		}
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var store fakePRStore
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", path, err)
	}
	if store.PRs == nil {
		store.PRs = map[string]ghPRView{}
	}
	return store
}

func saveFakePRStore(t *testing.T, path string, store fakePRStore) {
	t.Helper()
	if store.PRs == nil {
		store.PRs = map[string]ghPRView{}
	}
	encoded, err := json.Marshal(store)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func helperFlagValue(args []string, flag string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func helperHasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func helperHeadArg(args []string) string {
	if len(args) < 4 {
		return ""
	}
	switch args[2] {
	case "view":
		return helperFlagValue(args[3:], "--head")
	case "list":
		return helperFlagValue(args[3:], "--head")
	case "create":
		return helperFlagValue(args[3:], "--head")
	default:
		return ""
	}
}

func helperLookupPRForView(prs map[string]ghPRView, target string) (ghPRView, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ghPRView{}, false
	}
	if _, err := strconv.Atoi(target); err == nil {
		for _, pr := range prs {
			if strconv.Itoa(pr.Number) == target {
				return pr, true
			}
		}
		return ghPRView{}, false
	}
	if strings.Contains(target, ":") {
		return ghPRView{}, false
	}
	pr, ok := prs[target]
	return pr, ok
}
