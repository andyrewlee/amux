package computer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/term"
)

const (
	envAmuxSkipPreflight = "AMUX_SKIP_PREFLIGHT"
)

// RunPreflight validates required local dependencies for interactive sessions.
func RunPreflight(providerName string) error {
	if envIsOne(envAmuxSkipPreflight) {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	resolved := ResolveProviderName(cfg, providerName)
	if resolved == "" {
		return errors.New("provider is required. Use --provider or AMUX_PROVIDER")
	}
	switch resolved {
	case ProviderDaytona:
		if ResolveAPIKey(cfg) == "" {
			return errors.New("Daytona API key not found. Set AMUX_DAYTONA_API_KEY or run `amux auth login`.")
		}
		if _, err := exec.LookPath("ssh"); err != nil {
			return errors.New("ssh is required for interactive sessions. Install OpenSSH and try again.")
		}
	case ProviderSprites:
		if ResolveSpritesToken(cfg) == "" {
			return errors.New("Sprites token not found. Set AMUX_SPRITES_TOKEN or SPRITES_TOKEN.")
		}
	case ProviderDocker:
		if _, err := exec.LookPath("docker"); err != nil {
			return errors.New("docker is required for local computers. Install Docker and try again.")
		}
	default:
		return fmt.Errorf("provider %q is not supported", resolved)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("Interactive mode requires a TTY.")
	}
	return nil
}
