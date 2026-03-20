package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type openClawSyncLink struct {
	Profile   string `json:"profile"`
	Workspace string `json:"workspace"`
	SkillPath string `json:"skill_path"`
	Target    string `json:"target"`
}

type openClawSyncResult struct {
	SkillSource string             `json:"skill_source"`
	Links       []openClawSyncLink `json:"links"`
}

var (
	openClawLookPath    = exec.LookPath
	openClawExecCommand = exec.Command
)

func cmdDevOpenClawSync(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux dev openclaw-sync [--skill-src <path>] [--main-workspace <path>] [--dev-workspace <path>] [--skip-verify]"

	fs := newFlagSet("dev openclaw-sync")
	skillSrc := fs.String("skill-src", envOrDefault("OPENCLAW_AMUX_SKILL_SRC", filepath.Join(".", "skills", "amux")), "skill source path")
	mainFallback := envOrDefault("OPENCLAW_DEFAULT_WORKSPACE", filepath.Join(userHomeDir(), ".openclaw", "workspace"))
	devFallback := envOrDefault("OPENCLAW_DEV_WORKSPACE", filepath.Join(userHomeDir(), ".openclaw", "workspace-dev"))
	mainWorkspace := fs.String("main-workspace", mainFallback, "fallback main workspace path")
	devWorkspace := fs.String("dev-workspace", devFallback, "fallback dev workspace path")
	skipVerify := fs.Bool("skip-verify", false, "skip openclaw skills info verification")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if len(fs.Args()) > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}
	mainWorkspaceExplicit := false
	devWorkspaceExplicit := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "main-workspace":
			mainWorkspaceExplicit = true
		case "dev-workspace":
			devWorkspaceExplicit = true
		}
	})

	resolvedSkillSrc, err := filepath.Abs(*skillSrc)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("invalid --skill-src: %w", err))
	}
	info, err := os.Stat(resolvedSkillSrc)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("skill source not found: %w", err))
	}
	if !info.IsDir() {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--skill-src must point to a directory"))
	}

	openclawPath := ""
	if !*skipVerify || !mainWorkspaceExplicit || !devWorkspaceExplicit {
		openclawPath, err = openClawLookPath("openclaw")
		if err != nil {
			if gf.JSON {
				ReturnError(w, "missing_dependency", "openclaw CLI is required", nil, version)
			} else {
				Errorf(wErr, "openclaw CLI is required")
			}
			return ExitDependency
		}
	}

	mainResolved := resolveOpenClawWorkspace(openclawPath, false, *mainWorkspace, mainWorkspaceExplicit, mainFallback)
	devResolved := resolveOpenClawWorkspace(openclawPath, true, *devWorkspace, devWorkspaceExplicit, devFallback)
	result := openClawSyncResult{SkillSource: resolvedSkillSrc}

	for _, target := range []struct {
		profile   string
		workspace string
		dev       bool
	}{
		{profile: "main", workspace: mainResolved},
		{profile: "dev", workspace: devResolved, dev: true},
	} {
		link, err := syncOpenClawSkill(target.profile, target.workspace, resolvedSkillSrc)
		if err != nil {
			if gf.JSON {
				ReturnError(w, "openclaw_sync_failed", err.Error(), map[string]any{"profile": target.profile}, version)
			} else {
				Errorf(wErr, "%v", err)
			}
			return ExitInternalError
		}
		result.Links = append(result.Links, link)
		if !*skipVerify {
			if err := verifyOpenClawSkill(openclawPath, target.workspace, target.dev); err != nil {
				if gf.JSON {
					ReturnError(w, "openclaw_verify_failed", err.Error(), map[string]any{"profile": target.profile}, version)
				} else {
					Errorf(wErr, "%v", err)
				}
				return ExitInternalError
			}
		}
	}

	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}

	for _, link := range result.Links {
		fmt.Fprintf(w, "Linked %s -> %s\n", link.SkillPath, link.Target)
	}
	fmt.Fprintln(w, "OpenClaw amux skill synced.")
	return ExitOK
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func openclawConfigWorkspace(openclawPath string, dev bool, fallback string) string {
	args := []string{"config", "get", "agents.defaults.workspace"}
	if dev {
		args = append([]string{"--dev"}, args...)
	}
	cmd := openClawExecCommand(openclawPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return fallback
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func resolveOpenClawWorkspace(openclawPath string, dev bool, explicit string, explicitSet bool, fallback string) string {
	if explicitSet {
		trimmed := strings.TrimSpace(explicit)
		if trimmed != "" {
			return trimmed
		}
		return fallback
	}
	return openclawConfigWorkspace(openclawPath, dev, fallback)
}

func syncOpenClawSkill(profile, workspace, skillSrc string) (openClawSyncLink, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return openClawSyncLink{}, fmt.Errorf("%s workspace path is empty", profile)
	}
	skillRoot := filepath.Join(workspace, "skills")
	skillPath := filepath.Join(skillRoot, "amux")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		return openClawSyncLink{}, fmt.Errorf("create %s skills dir: %w", profile, err)
	}
	if info, err := os.Lstat(skillPath); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return openClawSyncLink{}, fmt.Errorf("%s skill path already exists and is not a symlink: %s", profile, skillPath)
		}
		if err := os.Remove(skillPath); err != nil {
			return openClawSyncLink{}, fmt.Errorf("remove existing %s skill link: %w", profile, err)
		}
	} else if !os.IsNotExist(err) {
		return openClawSyncLink{}, fmt.Errorf("inspect existing %s skill path: %w", profile, err)
	}
	if err := os.Symlink(skillSrc, skillPath); err != nil {
		return openClawSyncLink{}, fmt.Errorf("symlink %s skill: %w", profile, err)
	}
	return openClawSyncLink{
		Profile:   profile,
		Workspace: workspace,
		SkillPath: skillPath,
		Target:    skillSrc,
	}, nil
}

func verifyOpenClawSkill(openclawPath, workspace string, dev bool) error {
	args := []string{"skills", "info", "amux"}
	profile := "main"
	if strings.TrimSpace(workspace) != "" {
		args = append([]string{"--workspace", workspace}, args...)
	}
	if dev {
		args = append([]string{"--dev"}, args...)
		profile = "dev"
	}
	cmd := openClawExecCommand(openclawPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s OpenClaw verification failed: %s", profile, strings.TrimSpace(string(out)))
	}
	return nil
}
