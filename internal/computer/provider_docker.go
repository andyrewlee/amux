package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

const defaultDockerImage = "node:20-bookworm"

// DockerConfig configures the Docker provider.
type DockerConfig struct {
	DefaultImage string
}

type dockerProvider struct {
	config DockerConfig
}

func newDockerProvider(cfg DockerConfig) Provider {
	if cfg.DefaultImage == "" {
		cfg.DefaultImage = defaultDockerImage
	}
	return &dockerProvider{config: cfg}
}

func (p *dockerProvider) Name() string { return ProviderDocker }

func (p *dockerProvider) CreateComputer(ctx context.Context, config ComputerCreateConfig) (RemoteComputer, error) {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		return nil, errors.New("docker computer name is required")
	}

	if existing, err := p.GetComputer(ctx, name); err == nil {
		if existing.State() != StateStarted {
			_ = existing.Start(ctx)
		}
		return existing, nil
	}

	image := config.Snapshot
	if image == "" {
		image = p.config.DefaultImage
	}

	args := []string{"run", "-d", "--name", name}
	for k, v := range config.Labels {
		if k == "" {
			continue
		}
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range config.EnvVars {
		if k == "" {
			continue
		}
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}
	if len(config.Volumes) > 0 {
		volumeArgs, err := buildDockerVolumeArgs(config.Volumes)
		if err != nil {
			return nil, err
		}
		args = append(args, volumeArgs...)
	}
	args = append(args, image, "sleep", "infinity")

	out, err := runDocker(ctx, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already in use") {
			return p.GetComputer(ctx, name)
		}
		return nil, err
	}
	id := strings.TrimSpace(string(out))
	return &dockerComputer{
		id:     id,
		name:   name,
		labels: config.Labels,
		state:  StateStarted,
	}, nil
}

func (p *dockerProvider) GetComputer(ctx context.Context, id string) (RemoteComputer, error) {
	info, err := dockerInspect(ctx, id)
	if err != nil {
		return nil, err
	}
	return info.toComputer(), nil
}

func (p *dockerProvider) ListComputers(ctx context.Context) ([]RemoteComputer, error) {
	out, err := runDocker(ctx, "ps", "-a", "--format", "{{.ID}}\t{{.Names}}\t{{.Labels}}\t{{.Status}}")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	computers := []RemoteComputer{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 2 {
			continue
		}
		labels := parseDockerLabels("")
		if len(parts) > 2 {
			labels = parseDockerLabels(parts[2])
		}
		state := StateStopped
		if len(parts) > 3 && strings.HasPrefix(strings.ToLower(parts[3]), "up") {
			state = StateStarted
		}
		computers = append(computers, &dockerComputer{
			id:     parts[0],
			name:   parts[1],
			labels: labels,
			state:  state,
		})
	}
	return computers, nil
}

func (p *dockerProvider) DeleteComputer(ctx context.Context, id string) error {
	_, err := runDocker(ctx, "rm", "-f", id)
	return err
}

func (p *dockerProvider) Volumes() VolumeManager { return &dockerVolumeManager{} }

func (p *dockerProvider) Snapshots() SnapshotManager { return nil }

func (p *dockerProvider) SupportsFeature(feature ProviderFeature) bool {
	switch feature {
	case FeatureVolumes:
		return true
	default:
		return false
	}
}

type dockerComputer struct {
	id     string
	name   string
	labels map[string]string
	state  ComputerState
}

func (c *dockerComputer) ID() string {
	if c.name != "" {
		return c.name
	}
	return c.id
}

func (c *dockerComputer) State() ComputerState { return c.state }

func (c *dockerComputer) Labels() map[string]string {
	if c.labels == nil {
		return map[string]string{}
	}
	return c.labels
}

func (c *dockerComputer) Start(ctx context.Context) error {
	_, err := runDocker(ctx, "start", c.ID())
	if err == nil {
		c.state = StateStarted
	}
	return err
}

func (c *dockerComputer) Stop(ctx context.Context) error {
	_, err := runDocker(ctx, "stop", c.ID())
	if err == nil {
		c.state = StateStopped
	}
	return err
}

func (c *dockerComputer) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	for {
		info, err := dockerInspect(ctx, c.ID())
		if err != nil {
			return err
		}
		if info.State.Running {
			c.state = StateStarted
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func (c *dockerComputer) Exec(ctx context.Context, cmd string, opts *ExecOptions) (*ExecResult, error) {
	ctx, cancel := withTimeout(ctx, timeoutFromOpts(opts))
	defer cancel()

	args := []string{"exec"}
	if opts != nil && opts.Cwd != "" {
		args = append(args, "-w", opts.Cwd)
	}
	if opts != nil && len(opts.Env) > 0 {
		keys := sortedKeys(opts.Env)
		for _, key := range keys {
			args = append(args, "--env", fmt.Sprintf("%s=%s", key, opts.Env[key]))
		}
	}
	args = append(args, c.ID(), "sh", "-lc", cmd)

	execCmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	err := execCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}
	return &ExecResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (c *dockerComputer) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error) {
	ctx, cancel := withTimeout(ctx, timeoutFromOpts(opts))
	defer cancel()

	args := []string{"exec", "-i"}
	if shouldAllocateTTY(stdin) {
		args = append(args, "-t")
	}
	if opts != nil && opts.Cwd != "" {
		args = append(args, "-w", opts.Cwd)
	}
	if opts != nil && len(opts.Env) > 0 {
		keys := sortedKeys(opts.Env)
		for _, key := range keys {
			args = append(args, "--env", fmt.Sprintf("%s=%s", key, opts.Env[key]))
		}
	}
	args = append(args, c.ID(), "sh", "-lc", cmd)

	execCmd := exec.CommandContext(ctx, "docker", args...)
	execCmd.Stdin = stdin
	execCmd.Stdout = stdout
	execCmd.Stderr = stderr
	if err := execCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func (c *dockerComputer) UploadFile(ctx context.Context, localPath, remotePath string) error {
	containerPath := fmt.Sprintf("%s:%s", c.ID(), remotePath)
	_, err := runDocker(ctx, "cp", localPath, containerPath)
	return err
}

func (c *dockerComputer) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	containerPath := fmt.Sprintf("%s:%s", c.ID(), remotePath)
	_, err := runDocker(ctx, "cp", containerPath, localPath)
	return err
}

func (c *dockerComputer) GetPreviewURL(_ context.Context, _ int) (string, error) {
	return "", errors.New("preview URLs are not supported for docker computers")
}

func (c *dockerComputer) Refresh(ctx context.Context) error {
	info, err := dockerInspect(ctx, c.ID())
	if err != nil {
		return err
	}
	c.id = info.ID
	c.name = strings.TrimPrefix(info.Name, "/")
	c.labels = info.Config.Labels
	if info.State.Running {
		c.state = StateStarted
	} else {
		c.state = StateStopped
	}
	return nil
}

type dockerVolumeManager struct{}

func (v *dockerVolumeManager) Create(ctx context.Context, name string) (*VolumeInfo, error) {
	out, err := runDocker(ctx, "volume", "create", name)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{ID: strings.TrimSpace(string(out)), Name: name, State: "available"}, nil
}

func (v *dockerVolumeManager) Get(ctx context.Context, name string) (*VolumeInfo, error) {
	vol, err := dockerVolumeInspect(ctx, name)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{ID: vol.Name, Name: vol.Name, State: "available"}, nil
}

func (v *dockerVolumeManager) GetOrCreate(ctx context.Context, name string) (*VolumeInfo, error) {
	info, err := v.Get(ctx, name)
	if err == nil {
		return info, nil
	}
	return v.Create(ctx, name)
}

func (v *dockerVolumeManager) Delete(ctx context.Context, name string) error {
	_, err := runDocker(ctx, "volume", "rm", name)
	return err
}

func (v *dockerVolumeManager) List(ctx context.Context) ([]*VolumeInfo, error) {
	out, err := runDocker(ctx, "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	volumes := []*VolumeInfo{}
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		volumes = append(volumes, &VolumeInfo{ID: name, Name: name, State: "available"})
	}
	return volumes, nil
}

func (v *dockerVolumeManager) WaitReady(ctx context.Context, name string, _ time.Duration) (*VolumeInfo, error) {
	return v.GetOrCreate(ctx, name)
}

type dockerInspectInfo struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func (d dockerInspectInfo) toComputer() *dockerComputer {
	state := StateStopped
	if d.State.Running {
		state = StateStarted
	}
	return &dockerComputer{
		id:     d.ID,
		name:   strings.TrimPrefix(d.Name, "/"),
		labels: d.Config.Labels,
		state:  state,
	}
}

type dockerVolumeInfo struct {
	Name string `json:"Name"`
}

func dockerInspect(ctx context.Context, id string) (*dockerInspectInfo, error) {
	out, err := runDocker(ctx, "inspect", id)
	if err != nil {
		return nil, err
	}
	var entries []dockerInspectInfo
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("docker computer not found: %s", id)
	}
	return &entries[0], nil
}

func dockerVolumeInspect(ctx context.Context, name string) (*dockerVolumeInfo, error) {
	out, err := runDocker(ctx, "volume", "inspect", name)
	if err != nil {
		return nil, err
	}
	var entries []dockerVolumeInfo
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("docker volume not found: %s", name)
	}
	return &entries[0], nil
}

func runDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func buildDockerVolumeArgs(volumes []VolumeMount) ([]string, error) {
	args := []string{}
	for _, mount := range volumes {
		if mount.VolumeID == "" || mount.MountPath == "" {
			return nil, fmt.Errorf("invalid volume mount: volume ID and mount path required")
		}
		options := []string{
			"type=volume",
			fmt.Sprintf("src=%s", mount.VolumeID),
			fmt.Sprintf("dst=%s", mount.MountPath),
		}
		if mount.ReadOnly {
			options = append(options, "readonly")
		}
		if mount.Subpath != "" {
			options = append(options, fmt.Sprintf("volume-subpath=%s", mount.Subpath))
		}
		args = append(args, "--mount", strings.Join(options, ","))
	}
	return args, nil
}

func parseDockerLabels(raw string) map[string]string {
	labels := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			labels[parts[0]] = parts[1]
		}
	}
	return labels
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func shouldAllocateTTY(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
