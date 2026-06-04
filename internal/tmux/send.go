package tmux

import "time"

// enterDelay is the pause between sending text and sending Enter. TUI agents
// (notably Codex) process typed characters asynchronously; if the CR byte
// arrives before the TUI finishes inserting the text, it gets dropped.
// 50ms is enough for every tested agent while keeping sends snappy.
const enterDelay = 50 * time.Millisecond

// sendTextArgs builds the send-keys argv that delivers text literally. The -l
// flag forces literal mode and the -- guard ensures a leading-dash payload is
// not parsed as a flag. The raw sessionName is passed without a sessionTarget
// prefix.
func sendTextArgs(sessionName, text string) []string {
	return []string{"send-keys", "-l", "-t", sessionName, "--", text}
}

// sendEnterArgs builds the send-keys argv that delivers a carriage return as a
// hex byte (-H 0D) rather than the named "Enter" key, which some raw-mode TUIs
// (e.g. Cline) drop.
func sendEnterArgs(sessionName string) []string {
	return []string{"send-keys", "-H", "-t", sessionName, "0D"}
}

// SendKeys sends text to a tmux session via send-keys.
// If enter is true, an Enter key is sent after the text.
func SendKeys(sessionName, text string, enter bool, opts Options) error {
	if sessionName == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}

	cmd, cancel := tmuxCommand(opts, sendTextArgs(sessionName, text)...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		return err
	}

	if enter {
		// Brief pause so the TUI finishes processing the text insertion
		// before we deliver the carriage return.
		time.Sleep(enterDelay)

		enterCmd, enterCancel := tmuxCommand(opts, sendEnterArgs(sessionName)...)
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
