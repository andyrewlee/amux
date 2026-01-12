package computer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	sprites "github.com/superfly/sprites-go"
)

// SpritesConfig configures the Sprites provider client.
type SpritesConfig struct {
	Token  string
	APIURL string
}

type spritesProvider struct {
	client     *sprites.Client
	token      string
	baseURL    string
	httpClient *http.Client
}

func newSpritesProvider(cfg SpritesConfig) Provider {
	opts := []sprites.Option{}
	baseURL := strings.TrimSpace(cfg.APIURL)
	if baseURL != "" {
		opts = append(opts, sprites.WithBaseURL(baseURL))
	} else {
		baseURL = "https://api.sprites.dev"
	}
	client := sprites.New(cfg.Token, opts...)
	return &spritesProvider{
		client:     client,
		token:      cfg.Token,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *spritesProvider) Name() string { return ProviderSprites }

func (p *spritesProvider) CreateComputer(ctx context.Context, config ComputerCreateConfig) (RemoteComputer, error) {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		return nil, errors.New("sprite name is required")
	}

	var spriteConfig *sprites.SpriteConfig
	if config.Resources != nil {
		spriteConfig = &sprites.SpriteConfig{}
		if config.Resources.CPUCores > 0 {
			spriteConfig.CPUs = int(math.Ceil(float64(config.Resources.CPUCores)))
		}
		if config.Resources.MemoryGB > 0 {
			spriteConfig.RamMB = int(math.Ceil(float64(config.Resources.MemoryGB * 1024)))
		}
	}

	sprite, err := p.client.CreateSprite(ctx, name, spriteConfig)
	if err != nil {
		if isSpriteAlreadyExists(err) {
			return p.GetComputer(ctx, name)
		}
		return nil, err
	}
	created := &spritesComputer{
		client:     p.client,
		name:       sprite.Name(),
		sprite:     sprite,
		token:      p.token,
		baseURL:    p.baseURL,
		httpClient: p.httpClient,
		envVars:    cloneEnvVars(config.EnvVars),
	}
	_ = created.Refresh(ctx)
	return created, nil
}

func (p *spritesProvider) GetComputer(ctx context.Context, id string) (RemoteComputer, error) {
	name := strings.TrimSpace(id)
	if name == "" {
		return nil, errors.New("sprite name is required")
	}
	sprite, err := p.client.GetSprite(ctx, name)
	if err != nil {
		return nil, err
	}
	return &spritesComputer{
		client:     p.client,
		name:       sprite.Name(),
		sprite:     sprite,
		token:      p.token,
		baseURL:    p.baseURL,
		httpClient: p.httpClient,
		envVars:    nil,
	}, nil
}

func (p *spritesProvider) ListComputers(ctx context.Context) ([]RemoteComputer, error) {
	list, err := p.client.ListAllSprites(ctx, "amux-")
	if err != nil {
		return nil, err
	}
	out := make([]RemoteComputer, 0, len(list))
	for _, sprite := range list {
		out = append(out, &spritesComputer{
			client:     p.client,
			name:       sprite.Name(),
			sprite:     sprite,
			token:      p.token,
			baseURL:    p.baseURL,
			httpClient: p.httpClient,
			envVars:    nil,
		})
	}
	return out, nil
}

func (p *spritesProvider) DeleteComputer(ctx context.Context, id string) error {
	name := strings.TrimSpace(id)
	if name == "" {
		return errors.New("sprite name is required")
	}
	return p.client.DeleteSprite(ctx, name)
}

func (p *spritesProvider) Volumes() VolumeManager { return nil }

func (p *spritesProvider) Snapshots() SnapshotManager { return nil }

func (p *spritesProvider) SupportsFeature(feature ProviderFeature) bool {
	switch feature {
	case FeatureExecSessions, FeatureCheckpoints, FeatureNetworkPolicy, FeaturePreviewURLs, FeatureTCPProxy:
		return true
	default:
		return false
	}
}

type spritesComputer struct {
	client     *sprites.Client
	name       string
	sprite     *sprites.Sprite
	token      string
	baseURL    string
	httpClient *http.Client
	envVars    map[string]string
}

func (s *spritesComputer) ID() string { return s.name }

func (s *spritesComputer) State() ComputerState {
	status := ""
	if s.sprite != nil {
		status = strings.ToLower(strings.TrimSpace(s.sprite.Status))
	}
	switch status {
	case "running", "warm":
		return StateStarted
	case "cold", "stopped":
		return StateStopped
	case "":
		return StatePending
	default:
		return StateError
	}
}

func (s *spritesComputer) Labels() map[string]string {
	return map[string]string{
		"amux.provider": ProviderSprites,
	}
}

func (s *spritesComputer) Start(ctx context.Context) error {
	_, err := s.Exec(ctx, "true", nil)
	return err
}

func (s *spritesComputer) Stop(_ context.Context) error {
	return nil
}

func (s *spritesComputer) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	_, err := s.Exec(ctx, "true", nil)
	return err
}

func (s *spritesComputer) Exec(ctx context.Context, cmd string, opts *ExecOptions) (*ExecResult, error) {
	ctx, cancel := withTimeout(ctx, timeoutFromOpts(opts))
	defer cancel()

	command := s.commandContext(ctx, cmd, opts)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*sprites.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}
	return &ExecResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (s *spritesComputer) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error) {
	ctx, cancel := withTimeout(ctx, timeoutFromOpts(opts))
	defer cancel()

	command := s.commandContext(ctx, cmd, opts)
	command.SetTTY(true)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		if exitErr, ok := err.(*sprites.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func (s *spritesComputer) UploadFile(ctx context.Context, localPath, remotePath string) error {
	if err := s.ensureRemoteDir(ctx, remotePath); err != nil {
		return err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	command := s.commandContext(ctx, fmt.Sprintf("cat > %s", ShellQuote(remotePath)), nil)
	command.Stdin = file
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if exitErr, ok := err.(*sprites.ExitError); ok {
			return fmt.Errorf("upload failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return err
	}
	return nil
}

func (s *spritesComputer) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	command := s.commandContext(ctx, fmt.Sprintf("cat %s", ShellQuote(remotePath)), nil)
	command.Stdout = file
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if exitErr, ok := err.(*sprites.ExitError); ok {
			return fmt.Errorf("download failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return err
	}
	return nil
}

func (s *spritesComputer) GetPreviewURL(ctx context.Context, port int) (string, error) {
	if s.sprite == nil || s.sprite.URL == "" {
		if err := s.Refresh(ctx); err != nil {
			return "", err
		}
	}
	if s.sprite == nil || s.sprite.URL == "" {
		return "", errors.New("preview URL not available")
	}
	base := strings.TrimRight(s.sprite.URL, "/")
	if port <= 0 || port == 80 || port == 443 {
		return base, nil
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Sprintf("%s:%d", base, port), nil
	}
	u.Host = fmt.Sprintf("%s:%d", u.Hostname(), port)
	return u.String(), nil
}

func (s *spritesComputer) Refresh(ctx context.Context) error {
	sprite, err := s.client.GetSprite(ctx, s.name)
	if err != nil {
		return err
	}
	s.sprite = sprite
	return nil
}

func (s *spritesComputer) setDefaultEnv(env map[string]string) {
	s.envVars = cloneEnvVars(env)
}

func (s *spritesComputer) ListExecSessions(ctx context.Context) ([]ExecSession, error) {
	sessions, err := s.client.ListSessions(ctx, s.name)
	if err != nil {
		return nil, err
	}
	out := make([]ExecSession, 0, len(sessions))
	for _, sess := range sessions {
		entry := ExecSession{
			ID:        sess.ID,
			Command:   sess.Command,
			TTY:       sess.TTY,
			Active:    sess.IsActive,
			Workdir:   sess.Workdir,
			CreatedAt: sess.Created,
		}
		if sess.LastActivity != nil {
			entry.LastActivity = *sess.LastActivity
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *spritesComputer) AttachExecSession(ctx context.Context, id string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error) {
	if strings.TrimSpace(id) == "" {
		return 1, errors.New("exec session id is required")
	}
	tty := false
	if sessions, err := s.ListExecSessions(ctx); err == nil {
		for _, sess := range sessions {
			if sess.ID == id {
				tty = sess.TTY
				break
			}
		}
	}

	command := s.client.Sprite(s.name).AttachSessionContext(ctx, id)
	if tty {
		command.SetTTY(true)
	}
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		if exitErr, ok := err.(*sprites.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func (s *spritesComputer) KillExecSession(ctx context.Context, id string, signal string, timeout time.Duration) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("exec session id is required")
	}
	base := strings.TrimRight(s.baseURL, "/")
	reqURL, err := url.Parse(fmt.Sprintf("%s/v1/sprites/%s/exec/%s/kill", base, s.name, id))
	if err != nil {
		return err
	}
	q := reqURL.Query()
	if signal != "" {
		q.Set("signal", signal)
	}
	if timeout > 0 {
		q.Set("timeout", timeout.String())
	}
	reqURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.token))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kill exec session failed (status %d): %s", resp.StatusCode, string(data))
	}
	return nil
}

func (s *spritesComputer) CreateCheckpoint(ctx context.Context, comment string, onEvent func(CheckpointEvent)) (*CheckpointInfo, error) {
	stream, err := s.client.Sprite(s.name).CreateCheckpointWithComment(ctx, comment)
	if err != nil {
		return nil, err
	}
	err = stream.ProcessAll(func(msg *sprites.StreamMessage) error {
		if onEvent != nil {
			onEvent(CheckpointEvent{
				Type:      msg.Type,
				Message:   msg.Data,
				Timestamp: time.Now(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	checkpoints, err := s.client.Sprite(s.name).ListCheckpoints(ctx, "")
	if err != nil {
		return nil, err
	}
	if len(checkpoints) == 0 {
		return &CheckpointInfo{}, nil
	}
	latest := checkpoints[0]
	for _, cp := range checkpoints[1:] {
		if cp.CreateTime.After(latest.CreateTime) {
			latest = cp
		}
	}
	return &CheckpointInfo{
		ID:        latest.ID,
		CreatedAt: latest.CreateTime,
		Comment:   latest.Comment,
	}, nil
}

func (s *spritesComputer) ListCheckpoints(ctx context.Context) ([]CheckpointInfo, error) {
	checkpoints, err := s.client.Sprite(s.name).ListCheckpoints(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]CheckpointInfo, 0, len(checkpoints))
	for _, cp := range checkpoints {
		out = append(out, CheckpointInfo{
			ID:        cp.ID,
			CreatedAt: cp.CreateTime,
			Comment:   cp.Comment,
		})
	}
	return out, nil
}

func (s *spritesComputer) GetCheckpoint(ctx context.Context, id string) (*CheckpointInfo, error) {
	cp, err := s.client.Sprite(s.name).GetCheckpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	return &CheckpointInfo{
		ID:        cp.ID,
		CreatedAt: cp.CreateTime,
		Comment:   cp.Comment,
	}, nil
}

func (s *spritesComputer) RestoreCheckpoint(ctx context.Context, id string, onEvent func(CheckpointEvent)) error {
	stream, err := s.client.Sprite(s.name).RestoreCheckpoint(ctx, id)
	if err != nil {
		return err
	}
	return stream.ProcessAll(func(msg *sprites.StreamMessage) error {
		if onEvent != nil {
			onEvent(CheckpointEvent{
				Type:      msg.Type,
				Message:   msg.Data,
				Timestamp: time.Now(),
			})
		}
		return nil
	})
}

func (s *spritesComputer) GetNetworkPolicy(ctx context.Context) (*NetworkPolicy, error) {
	policy, err := s.client.Sprite(s.name).GetNetworkPolicy(ctx)
	if err != nil {
		return nil, err
	}
	out := &NetworkPolicy{Rules: make([]NetworkPolicyRule, 0, len(policy.Rules))}
	for _, rule := range policy.Rules {
		out.Rules = append(out.Rules, NetworkPolicyRule{
			Domain:  rule.Domain,
			Action:  rule.Action,
			Include: rule.Include,
		})
	}
	return out, nil
}

func (s *spritesComputer) SetNetworkPolicy(ctx context.Context, policy NetworkPolicy) error {
	in := &sprites.NetworkPolicy{Rules: make([]sprites.NetworkPolicyRule, 0, len(policy.Rules))}
	for _, rule := range policy.Rules {
		in.Rules = append(in.Rules, sprites.NetworkPolicyRule{
			Domain:  rule.Domain,
			Action:  rule.Action,
			Include: rule.Include,
		})
	}
	return s.client.Sprite(s.name).UpdateNetworkPolicy(ctx, in)
}

func (s *spritesComputer) OpenTCPProxy(ctx context.Context, host string, port int) (io.ReadWriteCloser, error) {
	if port <= 0 {
		return nil, fmt.Errorf("port must be > 0")
	}
	if host == "" {
		host = "localhost"
	}
	wsURL, err := buildSpritesProxyURL(s.baseURL, s.name)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", s.token))

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, err
	}

	initMsg := sprites.ProxyInitMessage{Host: host, Port: port}
	if err := conn.WriteJSON(&initMsg); err != nil {
		conn.Close()
		return nil, err
	}
	var response sprites.ProxyResponseMessage
	if err := conn.ReadJSON(&response); err != nil {
		conn.Close()
		return nil, err
	}
	if response.Status != "connected" {
		conn.Close()
		return nil, fmt.Errorf("proxy connection failed: %s", response.Status)
	}
	return &spritesProxyConn{conn: conn}, nil
}

func (s *spritesComputer) commandContext(ctx context.Context, cmd string, opts *ExecOptions) *sprites.Cmd {
	envVars := mergeEnvVars(s.envVars, nil)
	if opts != nil {
		envVars = mergeEnvVars(envVars, opts.Env)
	}
	if len(envVars) > 0 {
		if assignments := BuildEnvAssignments(envVars); assignments != "" {
			cmd = fmt.Sprintf("%s %s", assignments, cmd)
		}
	}
	sprite := s.client.Sprite(s.name)
	command := sprite.CommandContext(ctx, "sh", "-lc", cmd)
	if opts != nil && opts.Cwd != "" {
		command.Dir = opts.Cwd
	}
	return command
}

func (s *spritesComputer) ensureRemoteDir(ctx context.Context, remotePath string) error {
	if remotePath == "" {
		return errors.New("remote path is required")
	}
	dir := path.Dir(remotePath)
	if dir == "." || dir == "/" {
		return nil
	}
	_, err := s.Exec(ctx, SafeCommands.MkdirP(dir), nil)
	return err
}

func isSpriteAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 409") || strings.Contains(msg, "already exists")
}

func mergeEnvVars(base map[string]string, override map[string]string) map[string]string {
	if base == nil && override == nil {
		return nil
	}
	merged := map[string]string{}
	for k, v := range base {
		if k == "" {
			continue
		}
		merged[k] = v
	}
	for k, v := range override {
		if k == "" {
			continue
		}
		merged[k] = v
	}
	return merged
}

func cloneEnvVars(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func timeoutFromOpts(opts *ExecOptions) time.Duration {
	if opts == nil {
		return 0
	}
	return opts.Timeout
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining < timeout {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func buildSpritesProxyURL(baseURL, name string) (string, error) {
	if baseURL == "" {
		baseURL = "https://api.sprites.dev"
	}
	if strings.HasPrefix(baseURL, "http") {
		baseURL = "ws" + baseURL[4:]
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u.Path = fmt.Sprintf("/v1/sprites/%s/proxy", name)
	return u.String(), nil
}

type spritesProxyConn struct {
	conn *websocket.Conn
	buf  []byte
}

func (c *spritesProxyConn) Read(p []byte) (int, error) {
	for len(c.buf) == 0 {
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if msgType != websocket.BinaryMessage {
			continue
		}
		c.buf = data
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

func (c *spritesProxyConn) Write(p []byte) (int, error) {
	if err := c.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *spritesProxyConn) Close() error {
	return c.conn.Close()
}
