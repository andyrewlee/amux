package tmux

import "time"

// enterDelay is the pause between sending text and sending Enter. TUI agents
// (notably Codex) process typed characters asynchronously; if the CR byte
// arrives before the TUI finishes inserting the text, it gets dropped.
// 50ms is enough for every tested agent while keeping sends snappy.
const enterDelay = 50 * time.Millisecond

// SendKeys sends text to a tmux session via send-keys.
// If enter is true, an Enter key is sent after the text.
func SendKeys(sessionName, text string, enter bool, opts Options) error {
	if sessionName == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}

	args := []string{"send-keys", "-l", "-t", sessionName, "--", text}
	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		return err
	}

	if enter {
		// Brief pause so the TUI finishes processing the text insertion
		// before we deliver the carriage return.
		time.Sleep(enterDelay)

		// Use -H 0D (hex carriage return) instead of the named "Enter" key.
		// Some TUI agents (e.g. Cline) use raw terminal mode where the named
		// Enter key is dropped, but the raw CR byte is always delivered.
		enterCmd, enterCancel := tmuxCommand(opts, "send-keys", "-H", "-t", sessionName, "0D")
		defer enterCancel()
		return enterCmd.Run()
	}
	return nil
}

// SendInterrupt sends Ctrl-C to a tmux session.
func SendInterrupt(sessionName string, opts Options) error {
	if sessionName == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	cmd, cancel := tmuxCommand(opts, "send-keys", "-t", sessionName, "C-c")
	defer cancel()
	return cmd.Run()
}
