package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func promptSecret(label string) (string, error) {
	fmt.Fprint(cliStdout, label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		text, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(cliStdout)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(text)), nil
	}
	text, err := bufio.NewReader(os.Stdin).ReadString('\n')
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
		return errors.New("daytona API key not found; set AMUX_DAYTONA_API_KEY or run `amux auth login`")
	}
	apiKey, err := promptSecret("Daytona API key: ")
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("no API key provided")
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
