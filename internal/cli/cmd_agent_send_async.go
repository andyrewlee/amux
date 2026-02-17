package cli

import (
	"io"
	"os"
	"os/exec"
)

type sendJobProcessArgs struct {
	SessionName string
	AgentID     string
	Text        string
	Enter       bool
	JobID       string
}

func launchSendJobProcessor(args sendJobProcessArgs) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmdArgs := []string{"agent", "send"}
	if args.SessionName != "" {
		cmdArgs = append(cmdArgs, args.SessionName)
	} else if args.AgentID != "" {
		cmdArgs = append(cmdArgs, "--agent", args.AgentID)
	}
	cmdArgs = append(cmdArgs, "--text", args.Text, "--process-job", "--job-id", args.JobID)
	if args.Enter {
		cmdArgs = append(cmdArgs, "--enter")
	}

	cmd := exec.Command(exe, cmdArgs...)
	cmd.Env = os.Environ()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Stdin = nil
	return cmd.Start()
}
