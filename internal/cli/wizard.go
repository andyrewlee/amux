package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// WizardConfig holds the results of the setup wizard.
type WizardConfig struct {
	Agents         []string
	SyncSettings   bool
	SyncClaude     bool
	SyncGit        bool
	CreatePrebuilt bool
}

// RunFirstRunWizard guides new users through initial setup.
func RunFirstRunWizard() (*WizardConfig, error) {
	config := &WizardConfig{}

	fmt.Println()
	fmt.Println("\033[1m" + "Welcome to amux!" + "\033[0m")
	fmt.Println("Let's set up your sandbox environment.")
	fmt.Println()

	// Step 1: Check if Daytona API key is set
	if os.Getenv("DAYTONA_API_KEY") == "" {
		fmt.Println("\033[33m!\033[0m Daytona API key not found.")
		fmt.Println()
		fmt.Println("To get an API key:")
		fmt.Println("  1. Go to https://app.daytona.io/settings")
		fmt.Println("  2. Create a new API key")
		fmt.Println("  3. Run: export DAYTONA_API_KEY=your-key")
		fmt.Println()

		if !confirm("Do you have a Daytona API key ready to configure?") {
			fmt.Println()
			fmt.Println("You can run `amux setup` later to configure your API key.")
			return nil, fmt.Errorf("setup cancelled")
		}

		apiKey := prompt("Enter your Daytona API key:")
		if apiKey == "" {
			return nil, fmt.Errorf("API key is required")
		}

		// Save API key to shell profile
		if confirm("Save API key to your shell profile?") {
			if err := appendToShellProfile(fmt.Sprintf("export DAYTONA_API_KEY=%s", apiKey)); err != nil {
				fmt.Printf("\033[33m!\033[0m Could not save to profile: %v\n", err)
				fmt.Println("  Add this to your shell profile manually:")
				fmt.Printf("  export DAYTONA_API_KEY=%s\n", apiKey)
			} else {
				fmt.Println("\033[32m✓\033[0m API key saved to shell profile")
			}
		}

		// Set for current session
		os.Setenv("DAYTONA_API_KEY", apiKey)
	} else {
		fmt.Println("\033[32m✓\033[0m Daytona API key configured")
	}

	fmt.Println()

	// Step 2: Select agents
	fmt.Println("\033[1m[1/3] Which agents do you use?\033[0m")
	fmt.Println("Select the AI coding agents you want to use in sandboxes.")
	fmt.Println()

	agents := []struct {
		Name        string
		Description string
	}{
		{"claude", "Claude Code (Anthropic)"},
		{"codex", "Codex CLI (OpenAI)"},
		{"gemini", "Gemini CLI (Google)"},
		{"opencode", "OpenCode (open source)"},
		{"amp", "Amp (Sourcegraph)"},
		{"droid", "Droid (Factory)"},
	}

	selectedAgents := []string{}
	for _, agent := range agents {
		if confirm(fmt.Sprintf("  %s - %s?", agent.Name, agent.Description)) {
			selectedAgents = append(selectedAgents, agent.Name)
		}
	}

	if len(selectedAgents) == 0 {
		selectedAgents = []string{"claude"} // Default
		fmt.Println("  Defaulting to Claude Code")
	}

	config.Agents = selectedAgents
	fmt.Println()

	// Step 3: Settings sync
	fmt.Println("\033[1m[2/3] Sync local settings to sandbox?\033[0m")
	fmt.Println("This copies your preferences (NOT credentials) to the sandbox.")
	fmt.Println()

	config.SyncSettings = confirm("Enable settings sync?")
	if config.SyncSettings {
		// Check which settings exist locally
		status := sandbox.GetLocalSettingsStatus()

		if status[sandbox.AgentClaude] {
			config.SyncClaude = confirm("  Sync Claude settings (~/.claude/settings.json)?")
		}

		if status["git"] {
			config.SyncGit = confirm("  Sync Git config (~/.gitconfig - name, email, aliases only)?")
		}
	}

	fmt.Println()

	// Step 4: Prebuilt snapshot (optional, advanced)
	fmt.Println("\033[1m[3/3] Create a prebuilt snapshot?\033[0m")
	fmt.Println("Pre-installing agents in a snapshot makes startup faster.")
	fmt.Println("This takes a few minutes but only needs to be done once.")
	fmt.Println()

	config.CreatePrebuilt = confirm("Create prebuilt snapshot with selected agents?")

	fmt.Println()
	return config, nil
}

// ApplyWizardConfig applies the wizard configuration.
func ApplyWizardConfig(config *WizardConfig) error {
	// Save settings sync config
	if config.SyncSettings {
		syncCfg := sandbox.SettingsSyncConfig{
			Enabled: true,
			Claude:  config.SyncClaude,
			Git:     config.SyncGit,
		}

		if err := sandbox.SaveSettingsSyncConfig(syncCfg); err != nil {
			return fmt.Errorf("failed to save settings config: %w", err)
		}
		fmt.Println("\033[32m✓\033[0m Settings sync configured")
	}

	// Mark first run as complete
	cfg, _ := sandbox.LoadConfig()
	cfg.FirstRunComplete = true
	if err := sandbox.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("\033[32m✓ Setup complete!\033[0m")
	fmt.Println()
	fmt.Println("Quick start:")
	for _, agent := range config.Agents {
		fmt.Printf("  amux %s    # Run %s in a sandbox\n", agent, agent)
	}
	fmt.Println()
	fmt.Println("Other commands:")
	fmt.Println("  amux status     # Check sandbox status")
	fmt.Println("  amux doctor     # Run diagnostics")
	fmt.Println("  amux --help     # See all commands")
	fmt.Println()

	return nil
}

// ShouldRunWizard checks if the first-run wizard should be shown.
func ShouldRunWizard() bool {
	cfg, err := sandbox.LoadConfig()
	if err != nil {
		return true // Config doesn't exist
	}
	return !cfg.FirstRunComplete
}

// confirm asks a yes/no question and returns the answer.
func confirm(question string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", question)

	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	return answer == "y" || answer == "yes"
}

// prompt asks for text input and returns the answer.
func prompt(question string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s ", question)

	answer, _ := reader.ReadString('\n')
	return strings.TrimSpace(answer)
}

// promptWithDefault asks for text input with a default value.
// Kept for future use by setup wizard enhancements.
func promptWithDefault(question, defaultVal string) string { //nolint:unused
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [%s]: ", question, defaultVal)

	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)

	if answer == "" {
		return defaultVal
	}
	return answer
}

// selectOne presents a list of options and returns the selected one.
// Kept for future use by setup wizard enhancements.
func selectOne(question string, options []string) string { //nolint:unused
	fmt.Println(question)
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter number: ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)

		var idx int
		if _, err := fmt.Sscanf(answer, "%d", &idx); err == nil {
			if idx >= 1 && idx <= len(options) {
				return options[idx-1]
			}
		}
		fmt.Println("Invalid selection, try again.")
	}
}

// appendToShellProfile appends a line to the user's shell profile.
func appendToShellProfile(line string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Determine shell profile
	shell := os.Getenv("SHELL")
	var profilePath string

	switch {
	case strings.Contains(shell, "zsh"):
		profilePath = filepath.Join(home, ".zshrc")
	case strings.Contains(shell, "bash"):
		// Check for .bash_profile first (macOS), then .bashrc
		if _, err := os.Stat(filepath.Join(home, ".bash_profile")); err == nil {
			profilePath = filepath.Join(home, ".bash_profile")
		} else {
			profilePath = filepath.Join(home, ".bashrc")
		}
	case strings.Contains(shell, "fish"):
		profilePath = filepath.Join(home, ".config", "fish", "config.fish")
	default:
		profilePath = filepath.Join(home, ".profile")
	}

	// Append the line
	f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add newline before and after for safety
	_, err = f.WriteString(fmt.Sprintf("\n# Added by amux setup\n%s\n", line))
	return err
}

// PrintWelcomeBanner prints a welcome banner for new users.
func PrintWelcomeBanner() {
	fmt.Println()
	fmt.Println("  \033[1;36m╭─────────────────────────────────────────╮\033[0m")
	fmt.Println("  \033[1;36m│\033[0m                                         \033[1;36m│\033[0m")
	fmt.Println("  \033[1;36m│\033[0m   \033[1mamux\033[0m - AI Coding Agents in Computeres   \033[1;36m│\033[0m")
	fmt.Println("  \033[1;36m│\033[0m                                         \033[1;36m│\033[0m")
	fmt.Println("  \033[1;36m│\033[0m   Claude · Codex · Gemini · and more    \033[1;36m│\033[0m")
	fmt.Println("  \033[1;36m│\033[0m                                         \033[1;36m│\033[0m")
	fmt.Println("  \033[1;36m╰─────────────────────────────────────────╯\033[0m")
	fmt.Println()
}
