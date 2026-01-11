package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/daytona"
)

// HealthStatus represents the overall health of a sandbox.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthCheck represents a single health check.
type HealthCheck struct {
	Name        string
	Status      HealthStatus
	Message     string
	Duration    time.Duration
	Recoverable bool
	Details     map[string]string
}

// HealthReport contains all health check results.
type HealthReport struct {
	Overall   HealthStatus
	Checks    []HealthCheck
	Timestamp time.Time
	Duration  time.Duration
}

// SandboxHealth provides health checking and self-healing capabilities.
type SandboxHealth struct {
	sandbox *daytona.Sandbox
	client  *daytona.Daytona
	agent   Agent
	verbose bool
}

// NewSandboxHealth creates a new health checker for a sandbox.
func NewSandboxHealth(client *daytona.Daytona, sandbox *daytona.Sandbox, agent Agent) *SandboxHealth {
	return &SandboxHealth{
		sandbox: sandbox,
		client:  client,
		agent:   agent,
	}
}

// SetVerbose enables verbose output.
func (h *SandboxHealth) SetVerbose(verbose bool) {
	h.verbose = verbose
}

// Check performs all health checks and returns a report.
func (h *SandboxHealth) Check(ctx context.Context) *HealthReport {
	start := time.Now()
	report := &HealthReport{
		Overall:   HealthStatusHealthy,
		Checks:    make([]HealthCheck, 0),
		Timestamp: start,
	}

	// Run all checks
	checks := []func(context.Context) HealthCheck{
		h.checkSandboxState,
		h.checkVolumeMount,
		h.checkCredentialSymlinks,
		h.checkAgentInstalled,
		h.checkNetworkConnectivity,
		h.checkDiskSpace,
		h.checkProcesses,
	}

	for _, check := range checks {
		if ctx.Err() != nil {
			break
		}
		result := check(ctx)
		report.Checks = append(report.Checks, result)

		// Update overall status
		if result.Status == HealthStatusUnhealthy && report.Overall != HealthStatusUnhealthy {
			report.Overall = HealthStatusUnhealthy
		} else if result.Status == HealthStatusDegraded && report.Overall == HealthStatusHealthy {
			report.Overall = HealthStatusDegraded
		}
	}

	report.Duration = time.Since(start)
	return report
}

// checkSandboxState verifies the sandbox is running.
func (h *SandboxHealth) checkSandboxState(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "sandbox_state",
		Details: make(map[string]string),
	}

	// Simple echo test
	resp, err := h.sandbox.Process.ExecuteCommand("echo healthy")
	check.Duration = time.Since(start)

	if err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Cannot execute commands: %v", err)
		check.Recoverable = true
		return check
	}

	if resp.ExitCode != 0 {
		check.Status = HealthStatusUnhealthy
		check.Message = "Command execution failed"
		check.Recoverable = true
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = "Sandbox is responsive"
	check.Details["response_time"] = check.Duration.String()
	return check
}

// checkVolumeMount verifies the credentials volume is mounted.
func (h *SandboxHealth) checkVolumeMount(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "volume_mount",
		Details: make(map[string]string),
	}

	cmd := SafeCommands.Test("-d", CredentialsMountPath)
	resp, err := h.sandbox.Process.ExecuteCommand(cmd)
	check.Duration = time.Since(start)

	if err != nil || resp.ExitCode != 0 {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Credentials volume not mounted at %s", CredentialsMountPath)
		check.Recoverable = true
		check.Details["mount_path"] = CredentialsMountPath
		return check
	}

	// Check if we can write to it
	testFile := fmt.Sprintf("%s/.health_check", CredentialsMountPath)
	writeCmd := fmt.Sprintf("touch %s && rm %s", ShellQuote(testFile), ShellQuote(testFile))
	resp, err = h.sandbox.Process.ExecuteCommand(writeCmd)

	if err != nil || resp.ExitCode != 0 {
		check.Status = HealthStatusDegraded
		check.Message = "Credentials volume mounted but not writable"
		check.Recoverable = false
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = "Credentials volume mounted and writable"
	return check
}

// checkCredentialSymlinks verifies credential symlinks are set up.
func (h *SandboxHealth) checkCredentialSymlinks(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "credential_symlinks",
		Details: make(map[string]string),
	}

	if h.agent == AgentShell || h.agent == "" {
		check.Status = HealthStatusHealthy
		check.Message = "No credentials required for shell"
		check.Duration = time.Since(start)
		return check
	}

	plugin, ok := GetAgentPlugin(string(h.agent))
	if !ok {
		check.Status = HealthStatusUnknown
		check.Message = fmt.Sprintf("Unknown agent: %s", h.agent)
		check.Duration = time.Since(start)
		return check
	}

	credPaths := plugin.CredentialPaths()
	if len(credPaths) == 0 {
		check.Status = HealthStatusHealthy
		check.Message = "No credential paths configured"
		check.Duration = time.Since(start)
		return check
	}

	issues := []string{}
	for _, cred := range credPaths {
		homeDir := getSandboxHomeDir(h.sandbox)
		linkPath := fmt.Sprintf("%s/%s", homeDir, cred.HomePath)

		// Check if symlink exists
		cmd := fmt.Sprintf("readlink %s 2>/dev/null", ShellQuote(linkPath))
		resp, _ := h.sandbox.Process.ExecuteCommand(cmd)
		if resp == nil || resp.ExitCode != 0 {
			issues = append(issues, fmt.Sprintf("%s: not a symlink", cred.HomePath))
			continue
		}

		// Check if it points to the right place
		target := strings.TrimSpace(getStdoutFromResp(resp))
		expectedTarget := fmt.Sprintf("%s/%s", CredentialsMountPath, cred.VolumePath)
		if target != expectedTarget {
			issues = append(issues, fmt.Sprintf("%s: wrong target (%s)", cred.HomePath, target))
		}
	}

	check.Duration = time.Since(start)

	if len(issues) > 0 {
		check.Status = HealthStatusDegraded
		check.Message = fmt.Sprintf("Symlink issues: %s", strings.Join(issues, "; "))
		check.Recoverable = true
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = "All credential symlinks configured correctly"
	return check
}

// checkAgentInstalled verifies the agent is installed.
func (h *SandboxHealth) checkAgentInstalled(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "agent_installed",
		Details: make(map[string]string),
	}

	if h.agent == AgentShell || h.agent == "" {
		check.Status = HealthStatusHealthy
		check.Message = "Shell is always available"
		check.Duration = time.Since(start)
		return check
	}

	plugin, ok := GetAgentPlugin(string(h.agent))
	if !ok {
		check.Status = HealthStatusUnknown
		check.Message = fmt.Sprintf("Unknown agent: %s", h.agent)
		check.Duration = time.Since(start)
		return check
	}

	err := plugin.Validate(h.sandbox)
	check.Duration = time.Since(start)

	if err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Agent not installed: %v", err)
		check.Recoverable = true
		return check
	}

	// Get version if possible
	versionCmd := plugin.VersionCommand()
	if versionCmd != "" {
		resp, err := h.sandbox.Process.ExecuteCommand(versionCmd)
		if err == nil && resp.ExitCode == 0 {
			version := strings.TrimSpace(getStdoutFromResp(resp))
			if version != "" {
				check.Details["version"] = version
			}
		}
	}

	check.Status = HealthStatusHealthy
	check.Message = fmt.Sprintf("%s is installed", plugin.DisplayName())
	return check
}

// checkNetworkConnectivity verifies network access.
func (h *SandboxHealth) checkNetworkConnectivity(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "network",
		Details: make(map[string]string),
	}

	// Try to reach a reliable endpoint
	resp, err := h.sandbox.Process.ExecuteCommand("curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 https://api.anthropic.com")
	check.Duration = time.Since(start)

	if err != nil {
		check.Status = HealthStatusDegraded
		check.Message = "Network check failed (curl may not be available)"
		return check
	}

	statusCode := strings.TrimSpace(getStdoutFromResp(resp))
	check.Details["status_code"] = statusCode

	if resp.ExitCode != 0 {
		check.Status = HealthStatusDegraded
		check.Message = "Cannot reach external services"
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = "Network connectivity is working"
	return check
}

// checkDiskSpace verifies sufficient disk space.
func (h *SandboxHealth) checkDiskSpace(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "disk_space",
		Details: make(map[string]string),
	}

	resp, err := h.sandbox.Process.ExecuteCommand("df -h / | tail -1 | awk '{print $5}'")
	check.Duration = time.Since(start)

	if err != nil {
		check.Status = HealthStatusUnknown
		check.Message = "Could not check disk space"
		return check
	}

	usage := strings.TrimSpace(strings.TrimSuffix(getStdoutFromResp(resp), "%"))
	check.Details["usage"] = usage + "%"

	var usageInt int
	_, _ = fmt.Sscanf(usage, "%d", &usageInt)

	if usageInt >= 95 {
		check.Status = HealthStatusUnhealthy
		check.Message = fmt.Sprintf("Disk nearly full: %s%% used", usage)
		return check
	}

	if usageInt >= 80 {
		check.Status = HealthStatusDegraded
		check.Message = fmt.Sprintf("Disk usage high: %s%% used", usage)
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = fmt.Sprintf("Disk usage normal: %s%% used", usage)
	return check
}

// checkProcesses verifies no zombie processes or resource issues.
func (h *SandboxHealth) checkProcesses(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "processes",
		Details: make(map[string]string),
	}

	// Check for zombie processes
	resp, _ := h.sandbox.Process.ExecuteCommand("ps aux | grep -c ' Z '")
	check.Duration = time.Since(start)

	if resp != nil {
		zombies := strings.TrimSpace(getStdoutFromResp(resp))
		check.Details["zombie_count"] = zombies

		var zombieCount int
		_, _ = fmt.Sscanf(zombies, "%d", &zombieCount)

		if zombieCount > 10 {
			check.Status = HealthStatusDegraded
			check.Message = fmt.Sprintf("Many zombie processes detected: %d", zombieCount)
			return check
		}
	}

	check.Status = HealthStatusHealthy
	check.Message = "Process state is healthy"
	return check
}

// Repair attempts to fix common issues.
func (h *SandboxHealth) Repair(ctx context.Context) error {
	report := h.Check(ctx)

	var errors MultiError

	for _, check := range report.Checks {
		if check.Status != HealthStatusHealthy && check.Recoverable {
			if err := h.repairCheck(ctx, check); err != nil {
				errors.Add(fmt.Errorf("repair %s: %w", check.Name, err))
			}
		}
	}

	return errors.ErrorOrNil()
}

func (h *SandboxHealth) repairCheck(ctx context.Context, check HealthCheck) error {
	switch check.Name {
	case "sandbox_state":
		// Try to restart the sandbox
		if h.verbose {
			fmt.Println("Attempting to restart sandbox...")
		}
		if err := h.sandbox.Start(60 * time.Second); err != nil {
			return err
		}
		return nil

	case "volume_mount":
		// Re-setup credentials
		if h.verbose {
			fmt.Println("Re-setting up credentials volume...")
		}
		return SetupCredentials(h.client, h.sandbox, CredentialsConfig{
			Mode:  "sandbox",
			Agent: h.agent,
		}, h.verbose)

	case "credential_symlinks":
		// Re-create symlinks
		if h.verbose {
			fmt.Println("Re-creating credential symlinks...")
		}
		homeDir, _ := ensureCredentialDirs(h.sandbox)
		switch h.agent {
		case AgentClaude:
			prepareClaudeHome(h.sandbox, homeDir)
		case AgentCodex:
			prepareCodexHome(h.sandbox, homeDir)
		case AgentOpenCode:
			prepareOpenCodeHome(h.sandbox, homeDir)
		case AgentAmp:
			prepareAmpHome(h.sandbox, homeDir)
		case AgentGemini:
			prepareGeminiHome(h.sandbox, homeDir)
		case AgentDroid:
			prepareFactoryHome(h.sandbox, homeDir)
		}
		return nil

	case "agent_installed":
		// Re-install the agent
		if h.verbose {
			fmt.Printf("Re-installing %s...\n", h.agent)
		}
		return EnsureAgentInstalled(h.sandbox, h.agent, h.verbose, true)

	default:
		return fmt.Errorf("no repair strategy for %s", check.Name)
	}
}

// FormatReport returns a human-readable health report.
func FormatReport(report *HealthReport) string {
	var b strings.Builder

	// Overall status
	statusIcon := "?"
	switch report.Overall {
	case HealthStatusHealthy:
		statusIcon = "\033[32m✓\033[0m"
	case HealthStatusDegraded:
		statusIcon = "\033[33m!\033[0m"
	case HealthStatusUnhealthy:
		statusIcon = "\033[31m✗\033[0m"
	}

	b.WriteString(fmt.Sprintf("%s Overall: %s (checked in %s)\n\n", statusIcon, report.Overall, report.Duration.Round(time.Millisecond)))

	// Individual checks
	for _, check := range report.Checks {
		icon := "?"
		switch check.Status {
		case HealthStatusHealthy:
			icon = "\033[32m✓\033[0m"
		case HealthStatusDegraded:
			icon = "\033[33m!\033[0m"
		case HealthStatusUnhealthy:
			icon = "\033[31m✗\033[0m"
		case HealthStatusUnknown:
			icon = "\033[90m?\033[0m"
		}

		b.WriteString(fmt.Sprintf("  %s %s: %s", icon, check.Name, check.Message))
		if check.Duration > 0 {
			b.WriteString(fmt.Sprintf(" (%s)", check.Duration.Round(time.Millisecond)))
		}
		b.WriteString("\n")

		// Show details for non-healthy checks
		if check.Status != HealthStatusHealthy && len(check.Details) > 0 {
			for k, v := range check.Details {
				b.WriteString(fmt.Sprintf("    %s: %s\n", k, v))
			}
		}
	}

	return b.String()
}
