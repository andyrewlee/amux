package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const ghTimeout = 60 * time.Second

func findWorkspacePRByHead(repoPath, repoSelector, headBranch, headOwner string) (ghPRView, bool, error) {
	args := []string{"pr", "list", "--state", "open", "--head", strings.TrimSpace(headBranch), "--limit", "100"}
	if strings.TrimSpace(repoSelector) != "" {
		args = append(args, "--repo", strings.TrimSpace(repoSelector))
	}
	args = append(args, "--json", "number,url,title,body,headRefName,headRepositoryOwner,baseRefName,isDraft,state")
	out, err := runExternalCommand(repoPath, "gh", args...)
	if err != nil {
		return ghPRView{}, false, err
	}
	var views []ghPRView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		return ghPRView{}, false, err
	}
	headOwner = strings.TrimSpace(headOwner)
	for _, view := range views {
		if strings.TrimSpace(view.HeadRefName) != strings.TrimSpace(headBranch) {
			continue
		}
		if headOwner != "" && strings.TrimSpace(view.HeadRepositoryOwner.Login) != headOwner {
			continue
		}
		return view, true, nil
	}
	return ghPRView{}, false, nil
}

func ghFindPRByHead(repoPath, repoSelector, headBranch, headOwner string) (ghPRView, error) {
	view, ok, err := findWorkspacePRByHead(repoPath, repoSelector, headBranch, headOwner)
	if err != nil {
		return ghPRView{}, err
	}
	if !ok {
		return ghPRView{}, fmt.Errorf("no pull requests found for branch %s", strings.TrimSpace(headBranch))
	}
	return view, nil
}

func ghViewPRByNumber(repoPath, repoSelector string, number int) (ghPRView, error) {
	args := []string{"pr", "view", strconv.Itoa(number)}
	if strings.TrimSpace(repoSelector) != "" {
		args = append(args, "--repo", strings.TrimSpace(repoSelector))
	}
	args = append(args, "--json", "number,url,title,body,headRefName,baseRefName,isDraft,state")
	out, err := runExternalCommand(repoPath, "gh", args...)
	if err != nil {
		return ghPRView{}, err
	}
	var view ghPRView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		return ghPRView{}, err
	}
	return view, nil
}

func ghCreatePR(repoPath, repoSelector, headBranch, baseBranch, title, body string, draft bool, fallbackTitle string) error {
	args := []string{"pr", "create", "--base", strings.TrimSpace(baseBranch), "--head", strings.TrimSpace(headBranch)}
	if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
		args = append(args, "--fill")
	} else {
		if strings.TrimSpace(title) == "" {
			title = strings.TrimSpace(fallbackTitle)
		}
		args = append(args, "--title", title, "--body", body)
	}
	if draft {
		args = append(args, "--draft")
	}
	if strings.TrimSpace(repoSelector) != "" {
		args = append(args, "--repo", strings.TrimSpace(repoSelector))
	}
	_, err := runExternalCommand(repoPath, "gh", args...)
	return err
}

func ghEditPR(repoPath, repoSelector string, number int, baseBranch, title, body string) error {
	args := []string{"pr", "edit", strconv.Itoa(number), "--base", strings.TrimSpace(baseBranch)}
	if strings.TrimSpace(title) != "" {
		args = append(args, "--title", title)
	}
	if strings.TrimSpace(body) != "" {
		args = append(args, "--body", body)
	}
	if strings.TrimSpace(repoSelector) != "" {
		args = append(args, "--repo", strings.TrimSpace(repoSelector))
	}
	_, err := runExternalCommand(repoPath, "gh", args...)
	return err
}

func runExternalCommand(dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := cliExecCommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(cmd.Env) > 0 {
		cmd.Env = filterCommandEnv(cmd.Env)
	} else {
		cmd.Env = filteredCommandEnv()
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func filteredCommandEnv() []string {
	return filterCommandEnv(os.Environ())
}

func filterCommandEnv(input []string) []string {
	env := make([]string, 0, len(input))
	for _, entry := range input {
		if strings.HasPrefix(entry, "GIT_DIR=") ||
			strings.HasPrefix(entry, "GIT_WORK_TREE=") ||
			strings.HasPrefix(entry, "GIT_INDEX_FILE=") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
