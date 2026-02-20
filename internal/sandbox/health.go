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
	inner   *daytona.Sandbox
	client  *daytona.Daytona
	agent   Agent
	verbose bool
}

// NewSandboxHealth creates a new health checker for a sandbox.
func NewSandboxHealth(client *daytona.Daytona, sandboxHandle RemoteSandbox, agent Agent) (*SandboxHealth, error) {
	dc, ok := sandboxHandle.(*daytonaSandbox)
	if !ok {
		return nil, fmt.Errorf("sandbox provider does not support Daytona health checks")
	}
	return &SandboxHealth{
		inner:  dc.inner,
		client: client,
		agent:  agent,
	}, nil
}

// SetVerbose enables verbose output.
func (h *SandboxHealth) SetVerbose(verbose bool) {
	h.verbose = verbose
}

func (h *SandboxHealth) sandboxHandle() RemoteSandbox {
	return &daytonaSandbox{inner: h.inner}
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
		h.checkCredentialDirs,
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
	resp, err := execCommand(h.sandboxHandle(), "echo healthy", nil)
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

// checkCredentialDirs verifies credential directories exist in home.
func (h *SandboxHealth) checkCredentialDirs(ctx context.Context) HealthCheck {
	start := time.Now()
	check := HealthCheck{
		Name:    "credential_dirs",
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

	homeDir := getSandboxHomeDir(h.sandboxHandle())
	issues := []string{}
	for _, cred := range credPaths {
		dirPath := fmt.Sprintf("%s/%s", homeDir, cred.HomePath)

		// Check if directory exists
		cmd := SafeCommands.Test("-d", dirPath)
		resp, _ := execCommand(h.sandboxHandle(), cmd, nil)
		if resp == nil || resp.ExitCode != 0 {
			issues = append(issues, fmt.Sprintf("%s: directory missing", cred.HomePath))
		}
	}

	check.Duration = time.Since(start)

	if len(issues) > 0 {
		check.Status = HealthStatusDegraded
		check.Message = fmt.Sprintf("Credential directory issues: %s", strings.Join(issues, "; "))
		check.Recoverable = true
		return check
	}

	check.Status = HealthStatusHealthy
	check.Message = "All credential directories exist"
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

	err := plugin.Validate(h.sandboxHandle())
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
		resp, err := execCommand(h.sandboxHandle(), versionCmd, nil)
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
	resp, err := execCommand(h.sandboxHandle(), "curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 https://api.anthropic.com", nil)
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

	resp, err := execCommand(h.sandboxHandle(), "df -h / | tail -1 | awk '{print $5}'", nil)
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
	resp, _ := execCommand(h.sandboxHandle(), "ps aux | grep -c ' Z '", nil)
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
			fmt.Fprintln(sandboxStdout, "Attempting to restart sandbox...")
		}
		if err := h.inner.Start(60 * time.Second); err != nil {
			return err
		}
		return nil

	case "credential_dirs":
		// Re-create credential directories
		if h.verbose {
			fmt.Fprintln(sandboxStdout, "Re-creating credential directories...")
		}
		return SetupCredentials(h.sandboxHandle(), CredentialsConfig{
			Mode:  "sandbox",
			Agent: h.agent,
		}, h.verbose)

	case "agent_installed":
		// Re-install the agent
		if h.verbose {
			fmt.Fprintf(sandboxStdout, "Re-installing %s...\n", h.agent)
		}
		return EnsureAgentInstalled(h.sandboxHandle(), h.agent, h.verbose, true)

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
