package tmux

// SendKeys sends text to a tmux session via send-keys.
// If enter is true, an Enter key is sent after the text.
// Uses plain session name (not exactTarget) because send-keys expects a
// pane target and the "=" prefix is not recognized in that context.
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
		enterCmd, enterCancel := tmuxCommand(opts, "send-keys", "-t", sessionName, "Enter")
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
