package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PreflightCheck represents a single preflight check.
type PreflightCheck struct {
	Name        string
	Description string
	Required    bool // If true, failure blocks sandbox creation
	Check       func(ctx context.Context) PreflightResult
}

// PreflightResult contains the result of a preflight check.
type PreflightResult struct {
	Passed     bool
	Message    string
	Suggestion string
	Details    map[string]string
	Duration   time.Duration
}

// PreflightReport contains all preflight check results.
type PreflightReport struct {
	Passed   bool
	Checks   map[string]PreflightResult
	Duration time.Duration
	Errors   []string
	Warnings []string
}

// RunEnhancedPreflight performs comprehensive preflight checks.
func RunEnhancedPreflight(ctx context.Context, verbose bool) (*PreflightReport, error) {
	start := time.Now()
	report := &PreflightReport{
		Passed: true,
		Checks: make(map[string]PreflightResult),
	}

	checks := []PreflightCheck{
		{
			Name:        "api_key",
			Description: "Daytona API key configured",
			Required:    true,
			Check:       checkAPIKey,
		},
		{
			Name:        "ssh_available",
			Description: "SSH client available",
			Required:    true,
			Check:       checkSSHAvailable,
		},
		{
			Name:        "network_connectivity",
			Description: "Network connectivity to Daytona",
			Required:    true,
			Check:       checkNetworkConnectivity,
		},
		{
			Name:        "disk_space",
			Description: "Sufficient disk space",
			Required:    false,
			Check:       checkLocalDiskSpace,
		},
		{
			Name:        "git_available",
			Description: "Git available for workspace detection",
			Required:    false,
			Check:       checkGitAvailable,
		},
		{
			Name:        "node_available",
			Description: "Node.js available (for npm agents)",
			Required:    false,
			Check:       checkNodeAvailable,
		},
		{
			Name:        "terminal",
			Description: "Terminal is interactive",
			Required:    false,
			Check:       checkTerminal,
		},
		{
			Name:        "config_valid",
			Description: "Configuration file valid",
			Required:    false,
			Check:       checkConfigValid,
		},
	}

	for _, check := range checks {
		if ctx.Err() != nil {
			break
		}

		if verbose {
			fmt.Printf("Checking %s... ", check.Description)
		}

		result := check.Check(ctx)
		report.Checks[check.Name] = result

		if verbose {
			if result.Passed {
				fmt.Println("\033[32m✓\033[0m")
			} else if check.Required {
				fmt.Println("\033[31m✗\033[0m")
			} else {
				fmt.Println("\033[33m!\033[0m")
			}
		}

		if !result.Passed {
			if check.Required {
				report.Passed = false
				report.Errors = append(report.Errors, fmt.Sprintf("%s: %s", check.Name, result.Message))
			} else {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", check.Name, result.Message))
			}
		}
	}

	report.Duration = time.Since(start)
	return report, nil
}

func checkAPIKey(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		result.Passed = false
		result.Message = "DAYTONA_API_KEY environment variable not set"
		result.Suggestion = "Run `amux setup` to configure your API key"
		result.Duration = time.Since(start)
		return result
	}

	// Basic format validation
	if len(apiKey) < 20 {
		result.Passed = false
		result.Message = "API key appears to be invalid (too short)"
		result.Suggestion = "Check your API key at https://app.daytona.io/settings"
		result.Duration = time.Since(start)
		return result
	}

	result.Passed = true
	result.Message = "API key configured"
	result.Details["key_prefix"] = apiKey[:8] + "..."
	result.Duration = time.Since(start)
	return result
}

func checkSSHAvailable(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	path, err := exec.LookPath("ssh")
	if err != nil {
		result.Passed = false
		result.Message = "SSH client not found"
		result.Suggestion = "Install OpenSSH client"
		result.Duration = time.Since(start)
		return result
	}

	// Get SSH version
	cmd := exec.CommandContext(ctx, "ssh", "-V")
	output, _ := cmd.CombinedOutput()
	version := strings.TrimSpace(string(output))

	result.Passed = true
	result.Message = "SSH client available"
	result.Details["path"] = path
	if version != "" {
		result.Details["version"] = version
	}
	result.Duration = time.Since(start)
	return result
}

func checkNetworkConnectivity(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	// Check DNS resolution
	_, err := net.LookupHost("api.daytona.io")
	if err != nil {
		result.Passed = false
		result.Message = "Cannot resolve api.daytona.io"
		result.Suggestion = "Check your DNS settings and internet connection"
		result.Duration = time.Since(start)
		return result
	}

	// Check HTTP connectivity
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "HEAD", "https://api.daytona.io", nil)
	resp, err := client.Do(req)
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("Cannot connect to Daytona API: %v", err)
		result.Suggestion = "Check your firewall settings and internet connection"
		result.Duration = time.Since(start)
		return result
	}
	defer resp.Body.Close()

	result.Passed = true
	result.Message = "Network connectivity to Daytona working"
	result.Details["status"] = resp.Status
	result.Duration = time.Since(start)
	return result
}

func checkLocalDiskSpace(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	// Get temp directory
	tmpDir := os.TempDir()

	// Check available space (platform-specific)
	var availableGB float64
	var err error

	if runtime.GOOS == "windows" {
		// Windows - just skip this check
		result.Passed = true
		result.Message = "Disk space check skipped on Windows"
		result.Duration = time.Since(start)
		return result
	}

	// Unix-like systems
	cmd := exec.CommandContext(ctx, "df", "-BG", tmpDir)
	output, err := cmd.Output()
	if err != nil {
		// Try alternative format
		cmd = exec.CommandContext(ctx, "df", "-g", tmpDir)
		output, err = cmd.Output()
	}

	if err != nil {
		result.Passed = true // Don't fail on check errors
		result.Message = "Could not determine disk space"
		result.Duration = time.Since(start)
		return result
	}

	// Parse output
	lines := strings.Split(string(output), "\n")
	if len(lines) >= 2 {
		fields := strings.Fields(lines[1])
		if len(fields) >= 4 {
			_, _ = fmt.Sscanf(strings.TrimSuffix(fields[3], "G"), "%f", &availableGB)
		}
	}

	result.Details["temp_dir"] = tmpDir
	result.Details["available_gb"] = fmt.Sprintf("%.1f", availableGB)

	if availableGB < 1 {
		result.Passed = false
		result.Message = fmt.Sprintf("Low disk space: %.1fGB available", availableGB)
		result.Suggestion = "Free up disk space in your temp directory"
	} else if availableGB < 5 {
		result.Passed = true
		result.Message = fmt.Sprintf("Disk space is low: %.1fGB available", availableGB)
	} else {
		result.Passed = true
		result.Message = fmt.Sprintf("Sufficient disk space: %.1fGB available", availableGB)
	}

	result.Duration = time.Since(start)
	return result
}

func checkGitAvailable(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	path, err := exec.LookPath("git")
	if err != nil {
		result.Passed = false
		result.Message = "Git not found"
		result.Suggestion = "Install Git for better workspace detection"
		result.Duration = time.Since(start)
		return result
	}

	// Get git version
	cmd := exec.CommandContext(ctx, "git", "--version")
	output, _ := cmd.Output()
	version := strings.TrimSpace(string(output))

	result.Passed = true
	result.Message = "Git available"
	result.Details["path"] = path
	if version != "" {
		result.Details["version"] = version
	}
	result.Duration = time.Since(start)
	return result
}

func checkNodeAvailable(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	nodePath, nodeErr := exec.LookPath("node")
	npmPath, npmErr := exec.LookPath("npm")

	if nodeErr != nil && npmErr != nil {
		result.Passed = false
		result.Message = "Node.js not found locally"
		result.Suggestion = "Node.js is optional - agents will be installed in the sandbox"
		result.Duration = time.Since(start)
		return result
	}

	if nodePath != "" {
		result.Details["node_path"] = nodePath
		cmd := exec.CommandContext(ctx, "node", "--version")
		if output, err := cmd.Output(); err == nil {
			result.Details["node_version"] = strings.TrimSpace(string(output))
		}
	}

	if npmPath != "" {
		result.Details["npm_path"] = npmPath
		cmd := exec.CommandContext(ctx, "npm", "--version")
		if output, err := cmd.Output(); err == nil {
			result.Details["npm_version"] = strings.TrimSpace(string(output))
		}
	}

	result.Passed = true
	result.Message = "Node.js available"
	result.Duration = time.Since(start)
	return result
}

func checkTerminal(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	// Check if stdin is a terminal
	fi, err := os.Stdin.Stat()
	if err != nil {
		result.Passed = true
		result.Message = "Could not determine terminal status"
		result.Duration = time.Since(start)
		return result
	}

	isTerminal := (fi.Mode() & os.ModeCharDevice) != 0
	result.Details["is_terminal"] = fmt.Sprintf("%v", isTerminal)

	if term := os.Getenv("TERM"); term != "" {
		result.Details["TERM"] = term
	}

	if !isTerminal {
		result.Passed = false
		result.Message = "Not running in an interactive terminal"
		result.Suggestion = "Some features may not work correctly in non-interactive mode"
	} else {
		result.Passed = true
		result.Message = "Running in interactive terminal"
	}

	result.Duration = time.Since(start)
	return result
}

func checkConfigValid(ctx context.Context) PreflightResult {
	start := time.Now()
	result := PreflightResult{Details: make(map[string]string)}

	cfg, err := LoadConfig()
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("Config error: %v", err)
		result.Suggestion = "Run `amux setup` to fix configuration"
		result.Duration = time.Since(start)
		return result
	}

	// Get config path
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".amux", "config.json")
	result.Details["config_path"] = configPath

	if cfg.DefaultSnapshotName != "" {
		result.Details["default_snapshot"] = cfg.DefaultSnapshotName
	}

	result.Passed = true
	result.Message = "Configuration valid"
	result.Duration = time.Since(start)
	return result
}

// FormatPreflightReport returns a human-readable preflight report.
func FormatPreflightReport(report *PreflightReport) string {
	var b strings.Builder

	// Overall status
	if report.Passed {
		b.WriteString("\033[32m✓ Preflight checks passed\033[0m")
	} else {
		b.WriteString("\033[31m✗ Preflight checks failed\033[0m")
	}
	b.WriteString(fmt.Sprintf(" (completed in %s)\n\n", report.Duration.Round(time.Millisecond)))

	// Errors
	if len(report.Errors) > 0 {
		b.WriteString("\033[31mErrors:\033[0m\n")
		for _, err := range report.Errors {
			b.WriteString(fmt.Sprintf("  ✗ %s\n", err))
		}
		b.WriteString("\n")
	}

	// Warnings
	if len(report.Warnings) > 0 {
		b.WriteString("\033[33mWarnings:\033[0m\n")
		for _, warn := range report.Warnings {
			b.WriteString(fmt.Sprintf("  ! %s\n", warn))
		}
		b.WriteString("\n")
	}

	// Detailed results
	b.WriteString("Details:\n")
	for name, result := range report.Checks {
		icon := "\033[32m✓\033[0m"
		if !result.Passed {
			icon = "\033[31m✗\033[0m"
		}
		b.WriteString(fmt.Sprintf("  %s %s: %s\n", icon, name, result.Message))

		if result.Suggestion != "" && !result.Passed {
			b.WriteString(fmt.Sprintf("    Suggestion: %s\n", result.Suggestion))
		}
	}

	return b.String()
}

// QuickPreflight runs only the required checks for fast startup.
func QuickPreflight() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check API key
	if os.Getenv("DAYTONA_API_KEY") == "" {
		return NewSandboxError(ErrCodePreflight, "api_key", nil).
			WithSuggestion("Run `amux setup` to configure your Daytona API key")
	}

	// Check SSH
	if _, err := exec.LookPath("ssh"); err != nil {
		return NewSandboxError(ErrCodePreflight, "ssh", err).
			WithSuggestion("Install OpenSSH client")
	}

	// Quick network check (with short timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "HEAD", "https://api.daytona.io", nil)
	if _, err := client.Do(req); err != nil {
		return NewSandboxError(ErrCodeNetwork, "connectivity", err).
			WithSuggestion("Check your internet connection")
	}

	return nil
}
