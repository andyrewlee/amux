package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
)

// BuildSSHCommand returns an ssh exec.Cmd for the sandbox plus a cleanup func to revoke access.
// Only Dayt0na sandboxes currently support SSH access.
func BuildSSHCommand(sb RemoteSandbox, remoteCommand string) (*exec.Cmd, func(), error) {
	ds, ok := sb.(*daytonaSandbox)
	if !ok || ds == nil || ds.inner == nil {
		return nil, nil, errors.New("SSH access is only supported for Daytona sandboxes")
	}

	sshAccess, err := ds.inner.CreateSshAccess(60)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = ds.inner.RevokeSshAccess(sshAccess.Token)
	}

	runnerDomain, err := waitForSshAccessDaytona(ds.inner, sshAccess.Token)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	sshHost := runnerDomain
	if sshHost == "" {
		sshHost = getSSHHost()
	}
	target := fmt.Sprintf("%s@%s", sshAccess.Token, sshHost)

	sshArgs := []string{
		"-tt",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		target,
	}
	if remoteCommand != "" {
		sshArgs = append(sshArgs, remoteCommand)
	}

	cmd := exec.Command("ssh", sshArgs...)
	return cmd, cleanup, nil
}
