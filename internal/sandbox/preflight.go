package sandbox

import (
	"errors"
	"os"
	"os/exec"

	"golang.org/x/term"
)

const (
	envAmuxSkipPreflight = "AMUX_SKIP_PREFLIGHT"
)

// RunPreflight validates required local dependencies for interactive sessions.
func RunPreflight() error {
	if envIsOne(envAmuxSkipPreflight) {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if ResolveAPIKey(cfg) == "" {
		return errors.New("daytona API key not found; set AMUX_DAYTONA_API_KEY or run `amux auth login`")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return errors.New("ssh is required for interactive sessions; install OpenSSH and try again")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("interactive mode requires a TTY")
	}
	return nil
}
