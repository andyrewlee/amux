package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func promptInput(label string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(cliStdout, label)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func ensureDaytonaAPIKey() error {
	cfg, err := sandbox.LoadConfig()
	if err != nil {
		return err
	}
	if sandbox.ResolveAPIKey(cfg) != "" {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("Daytona API key not found. Set AMUX_DAYTONA_API_KEY or run `amux auth login`.")
	}
	apiKey, err := promptInput("Daytona API key: ")
	if err != nil {
		return err
	}
	if apiKey == "" {
		return fmt.Errorf("no API key provided")
	}
	cfg.DaytonaAPIKey = apiKey
	if err := sandbox.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Fprintln(cliStdout, "Saved Daytona API key to ~/.amux/config.json")
	return nil
}

func resolveSandboxHome(sb sandbox.RemoteSandbox) string {
	resp, err := sb.Exec(context.Background(), `sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`, nil)
	if err == nil {
		if resp.Stdout != "" {
			return strings.TrimSpace(resp.Stdout)
		}
	}
	return "/home/daytona"
}
