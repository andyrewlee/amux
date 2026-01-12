package computer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/daytona"
)

type daytonaProvider struct {
	client *daytona.Daytona
}

func newDaytonaProvider(client *daytona.Daytona) Provider {
	return &daytonaProvider{client: client}
}

func (p *daytonaProvider) Name() string { return ProviderDaytona }

func (p *daytonaProvider) CreateComputer(ctx context.Context, config ComputerCreateConfig) (RemoteComputer, error) {
	params := &daytona.CreateSandboxParams{
		Language:         "typescript",
		Snapshot:         config.Snapshot,
		EnvVars:          config.EnvVars,
		Labels:           config.Labels,
		AutoStopInterval: config.AutoStopMinutes,
	}
	if len(config.Volumes) > 0 {
		volumes := make([]daytona.VolumeMount, 0, len(config.Volumes))
		for _, mount := range config.Volumes {
			volumes = append(volumes, daytona.VolumeMount{
				VolumeID:  mount.VolumeID,
				MountPath: mount.MountPath,
				Subpath:   mount.Subpath,
			})
		}
		params.Volumes = volumes
	}

	sb, err := p.client.Create(params, nil)
	if err != nil {
		return nil, err
	}
	return &daytonaComputer{inner: sb}, nil
}

func (p *daytonaProvider) GetComputer(ctx context.Context, id string) (RemoteComputer, error) {
	sb, err := p.client.Get(id)
	if err != nil {
		return nil, err
	}
	return &daytonaComputer{inner: sb}, nil
}

func (p *daytonaProvider) ListComputers(ctx context.Context) ([]RemoteComputer, error) {
	sandboxes, err := p.client.List()
	if err != nil {
		return nil, err
	}
	out := make([]RemoteComputer, 0, len(sandboxes))
	for _, sb := range sandboxes {
		out = append(out, &daytonaComputer{inner: sb})
	}
	return out, nil
}

func (p *daytonaProvider) DeleteComputer(ctx context.Context, id string) error {
	sb, err := p.client.Get(id)
	if err != nil {
		return err
	}
	return p.client.Delete(sb)
}

func (p *daytonaProvider) Volumes() VolumeManager {
	return &daytonaVolumeManager{client: p.client}
}

func (p *daytonaProvider) Snapshots() SnapshotManager {
	return &daytonaSnapshotManager{client: p.client}
}

func (p *daytonaProvider) SupportsFeature(feature ProviderFeature) bool {
	switch feature {
	case FeatureVolumes, FeatureSnapshots, FeaturePreviewURLs, FeatureSSHAccess, FeatureDesktop:
		return true
	default:
		return false
	}
}

type daytonaComputer struct {
	inner *daytona.Sandbox
}

func (s *daytonaComputer) ID() string { return s.inner.ID }

func (s *daytonaComputer) State() ComputerState {
	switch s.inner.State {
	case "pending":
		return StatePending
	case "started":
		return StateStarted
	case "stopped":
		return StateStopped
	case "error":
		return StateError
	default:
		return ComputerState(s.inner.State)
	}
}

func (s *daytonaComputer) Labels() map[string]string { return s.inner.Labels }

func (s *daytonaComputer) Start(ctx context.Context) error {
	timeout := timeoutFromContext(ctx, 60*time.Second)
	return s.inner.Start(timeout)
}

func (s *daytonaComputer) Stop(ctx context.Context) error {
	timeout := timeoutFromContext(ctx, 60*time.Second)
	return s.inner.Stop(timeout)
}

func (s *daytonaComputer) WaitReady(ctx context.Context, timeout time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout || timeout == 0 {
			timeout = remaining
		}
	}
	return s.inner.WaitUntilStarted(timeout)
}

func (s *daytonaComputer) Exec(ctx context.Context, cmd string, opts *ExecOptions) (*ExecResult, error) {
	var options daytona.ExecuteCommandOptions
	if opts != nil {
		options.Cwd = opts.Cwd
		options.Env = opts.Env
		options.Timeout = opts.Timeout
	}
	resp, err := s.inner.Process.ExecuteCommand(cmd, options)
	if err != nil {
		return nil, err
	}
	result := &ExecResult{ExitCode: int(resp.ExitCode)}
	if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
		result.Stdout = resp.Artifacts.Stdout
	} else {
		result.Stdout = resp.Result
	}
	return result, nil
}

func (s *daytonaComputer) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error) {
	sshAccess, err := s.inner.CreateSshAccess(60)
	if err != nil {
		return 1, err
	}
	defer func() { _ = s.inner.RevokeSshAccess(sshAccess.Token) }()

	runnerDomain, err := waitForSshAccessDaytona(s.inner, sshAccess.Token)
	if err != nil {
		return 1, err
	}
	sshHost := runnerDomain
	if sshHost == "" {
		sshHost = getSSHHost()
	}
	target := fmt.Sprintf("%s@%s", sshAccess.Token, sshHost)

	remoteCommand := cmd
	if opts != nil {
		if len(opts.Env) > 0 {
			exports := BuildEnvExports(opts.Env)
			if len(exports) > 0 {
				remoteCommand = strings.Join(exports, "; ") + "; " + remoteCommand
			}
		}
		if opts.Cwd != "" {
			remoteCommand = fmt.Sprintf("cd %s && %s", ShellQuote(opts.Cwd), remoteCommand)
		}
	}
	remoteCommand = fmt.Sprintf("bash -lc %s", ShellQuote(remoteCommand))

	sshArgs := []string{
		"-tt",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		target,
		remoteCommand,
	}

	cmdExec := exec.Command("ssh", sshArgs...)
	cmdExec.Stdin = stdin
	cmdExec.Stdout = stdout
	cmdExec.Stderr = stderr
	if err := cmdExec.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 1, errors.New("ssh is required to run interactive sessions. Install OpenSSH and try again.")
		}
		return 1, err
	}
	if err := cmdExec.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func (s *daytonaComputer) UploadFile(ctx context.Context, localPath, remotePath string) error {
	return s.inner.FS.UploadFile(localPath, remotePath, timeoutFromContext(ctx, 0))
}

func (s *daytonaComputer) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	return s.inner.FS.DownloadFileTo(remotePath, localPath, timeoutFromContext(ctx, 0))
}

func (s *daytonaComputer) GetPreviewURL(ctx context.Context, port int) (string, error) {
	preview, err := s.inner.GetPreviewLink(port)
	if err != nil {
		return "", err
	}
	if preview == nil || preview.URL == "" {
		return "", nil
	}
	if preview.Token == "" || strings.Contains(preview.URL, "DAYTONA_SANDBOX_AUTH_KEY=") {
		return preview.URL, nil
	}
	separator := "?"
	if strings.Contains(preview.URL, "?") {
		separator = "&"
	}
	return preview.URL + separator + "DAYTONA_SANDBOX_AUTH_KEY=" + preview.Token, nil
}

func (s *daytonaComputer) Refresh(ctx context.Context) error {
	return s.inner.RefreshData()
}

// Optional interfaces for richer CLI output.
func (s *daytonaComputer) CPUCores() float32 { return s.inner.CPU }
func (s *daytonaComputer) MemoryGB() float32 { return s.inner.Memory }

// Desktop support.
func (s *daytonaComputer) DesktopStatus(ctx context.Context) (*DesktopStatus, error) {
	status, err := s.inner.GetComputerUseStatus()
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	return &DesktopStatus{Status: status.Status}, nil
}

func (s *daytonaComputer) StartDesktop(ctx context.Context) error {
	_, err := s.inner.StartComputerUse()
	return err
}

func (s *daytonaComputer) StopDesktop(ctx context.Context) error {
	_, err := s.inner.StopComputerUse()
	return err
}

func timeoutFromContext(ctx context.Context, fallback time.Duration) time.Duration {
	if ctx == nil {
		return fallback
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0
		}
		if fallback == 0 || remaining < fallback {
			return remaining
		}
	}
	return fallback
}

type daytonaVolumeManager struct {
	client *daytona.Daytona
}

func (v *daytonaVolumeManager) Create(ctx context.Context, name string) (*VolumeInfo, error) {
	volume, err := v.client.Volume.Get(name, true)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{ID: volume.ID, Name: volume.Name, State: volume.State}, nil
}

func (v *daytonaVolumeManager) Get(ctx context.Context, name string) (*VolumeInfo, error) {
	volume, err := v.client.Volume.Get(name, false)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{ID: volume.ID, Name: volume.Name, State: volume.State}, nil
}

func (v *daytonaVolumeManager) GetOrCreate(ctx context.Context, name string) (*VolumeInfo, error) {
	volume, err := v.client.Volume.Get(name, true)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{ID: volume.ID, Name: volume.Name, State: volume.State}, nil
}

func (v *daytonaVolumeManager) Delete(ctx context.Context, name string) error {
	return fmt.Errorf("volume delete is not supported by the Daytona API")
}

func (v *daytonaVolumeManager) List(ctx context.Context) ([]*VolumeInfo, error) {
	return nil, fmt.Errorf("volume listing is not supported by the Daytona API")
}

func (v *daytonaVolumeManager) WaitReady(ctx context.Context, name string, timeout time.Duration) (*VolumeInfo, error) {
	options := &daytona.VolumeWaitOptions{}
	if timeout > 0 {
		options.Timeout = timeout
	}
	volume, err := v.client.Volume.WaitForReady(name, options)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{ID: volume.ID, Name: volume.Name, State: volume.State}, nil
}

type daytonaSnapshotManager struct {
	client *daytona.Daytona
}

func (s *daytonaSnapshotManager) Create(ctx context.Context, name string, baseImage string, onLogs func(string)) (*SnapshotInfo, error) {
	image := BuildSnapshotImage(DefaultSnapshotAgents, baseImage)
	snap, err := s.client.Snapshot.Create(daytona.CreateSnapshotParams{
		Name:  name,
		Image: image,
	}, &daytona.SnapshotCreateOptions{OnLogs: onLogs})
	if err != nil {
		return nil, err
	}
	return &SnapshotInfo{ID: snap.ID, Name: snap.Name, State: snap.State}, nil
}

func (s *daytonaSnapshotManager) Get(ctx context.Context, name string) (*SnapshotInfo, error) {
	snapshots, err := s.client.Snapshot.List()
	if err != nil {
		return nil, err
	}
	for _, snap := range snapshots {
		if snap.Name == name {
			return &SnapshotInfo{ID: snap.ID, Name: snap.Name, State: snap.State}, nil
		}
	}
	return nil, fmt.Errorf("snapshot %q not found", name)
}

func (s *daytonaSnapshotManager) Delete(ctx context.Context, name string) error {
	return fmt.Errorf("snapshot delete is not supported by the Daytona API")
}

func (s *daytonaSnapshotManager) List(ctx context.Context) ([]*SnapshotInfo, error) {
	snapshots, err := s.client.Snapshot.List()
	if err != nil {
		return nil, err
	}
	out := make([]*SnapshotInfo, 0, len(snapshots))
	for _, snap := range snapshots {
		out = append(out, &SnapshotInfo{ID: snap.ID, Name: snap.Name, State: snap.State})
	}
	return out, nil
}
