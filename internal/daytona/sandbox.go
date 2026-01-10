package daytona

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

type sandboxDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Snapshot    *string           `json:"snapshot,omitempty"`
	Env         map[string]string `json:"env"`
	Labels      map[string]string `json:"labels"`
	State       string            `json:"state"`
	ErrorReason *string           `json:"errorReason,omitempty"`
	CPU         float32           `json:"cpu"`
	Memory      float32           `json:"memory"`
}

// Sandbox represents a remote sandbox.
type Sandbox struct {
	ID          string
	Name        string
	Snapshot    string
	Env         map[string]string
	Labels      map[string]string
	State       string
	ErrorReason string
	CPU         float32
	Memory      float32

	FS      *FileSystem
	Process *Process

	client *Daytona

	toolboxOnce sync.Once
	toolbox     *toolboxClient
	toolboxErr  error
}

func newSandboxFromDTO(dto *sandboxDTO, client *Daytona) *Sandbox {
	s := &Sandbox{client: client}
	if dto != nil {
		s.applyDTO(dto)
	}
	provider := func() (*toolboxClient, error) {
		return s.toolboxClient()
	}
	s.FS = &FileSystem{toolbox: provider}
	s.Process = &Process{toolbox: provider}
	return s
}

func (s *Sandbox) applyDTO(dto *sandboxDTO) {
	s.ID = dto.ID
	s.Name = dto.Name
	s.Snapshot = ""
	if dto.Snapshot != nil {
		s.Snapshot = *dto.Snapshot
	}
	s.Env = dto.Env
	s.Labels = dto.Labels
	s.State = dto.State
	s.ErrorReason = ""
	if dto.ErrorReason != nil {
		s.ErrorReason = *dto.ErrorReason
	}
	s.CPU = dto.CPU
	s.Memory = dto.Memory
}

func (s *Sandbox) toolboxClient() (*toolboxClient, error) {
	s.toolboxOnce.Do(func() {
		base, err := s.client.getProxyToolboxURL(context.Background())
		if err != nil {
			s.toolboxErr = err
			return
		}
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		s.toolbox = newToolboxClient(base+s.ID, s.client.headers, 24*time.Hour)
	})
	if s.toolboxErr != nil {
		return nil, s.toolboxErr
	}
	return s.toolbox, nil
}

// Start starts the sandbox and waits for it to be ready.
func (s *Sandbox) Start(timeout time.Duration) error {
	if timeout < 0 {
		return &APIError{StatusCode: 0, Message: "timeout must be non-negative"}
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var dto sandboxDTO
	if err := s.client.doRequest(ctx, httpMethodPost, "/sandbox/"+url.PathEscape(s.ID)+"/start", nil, nil, &dto); err != nil {
		return err
	}
	s.applyDTO(&dto)
	return s.WaitUntilStarted(timeout)
}

// Stop stops the sandbox.
func (s *Sandbox) Stop(timeout time.Duration) error {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return s.client.doRequest(ctx, httpMethodPost, "/sandbox/"+url.PathEscape(s.ID)+"/stop", nil, nil, nil)
}

// WaitUntilStarted waits until sandbox reaches started state.
func (s *Sandbox) WaitUntilStarted(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := s.RefreshData(); err != nil {
			return err
		}
		if s.State == "started" {
			return nil
		}
		if s.State == "error" {
			return &APIError{StatusCode: 0, Message: "sandbox failed to start: " + s.ErrorReason}
		}
		if timeout > 0 && time.Now().After(deadline) {
			return &APIError{StatusCode: 0, Message: "sandbox failed to become ready within timeout"}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// RefreshData refreshes sandbox data from the API.
func (s *Sandbox) RefreshData() error {
	var dto sandboxDTO
	if err := s.client.doJSON(context.Background(), httpMethodGet, "/sandbox/"+url.PathEscape(s.ID), nil, &dto); err != nil {
		return err
	}
	s.applyDTO(&dto)
	return nil
}

// CreateSshAccess creates SSH access for the sandbox.
func (s *Sandbox) CreateSshAccess(expiresInMinutes int32) (*SshAccess, error) {
	query := url.Values{}
	if expiresInMinutes > 0 {
		query.Set("expiresInMinutes", fmt.Sprintf("%d", expiresInMinutes))
	}
	var dto SshAccess
	if err := s.client.doRequest(context.Background(), httpMethodPost, "/sandbox/"+url.PathEscape(s.ID)+"/ssh-access", query, nil, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

// RevokeSshAccess revokes SSH access.
func (s *Sandbox) RevokeSshAccess(token string) error {
	query := url.Values{}
	if token != "" {
		query.Set("token", token)
	}
	return s.client.doRequest(context.Background(), httpMethodDelete, "/sandbox/"+url.PathEscape(s.ID)+"/ssh-access", query, nil, nil)
}

// ValidateSshAccess validates SSH access token.
func (s *Sandbox) ValidateSshAccess(token string) (*SshAccessValidation, error) {
	query := url.Values{}
	query.Set("token", token)
	var dto SshAccessValidation
	if err := s.client.doRequest(context.Background(), httpMethodGet, "/sandbox/ssh-access/validate", query, nil, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

// GetPreviewLink returns a preview URL and token for a sandbox port.
func (s *Sandbox) GetPreviewLink(port int) (*PortPreview, error) {
	var resp struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	path := fmt.Sprintf("/sandbox/%s/ports/%d/preview-url", url.PathEscape(s.ID), port)
	if err := s.client.doJSON(context.Background(), httpMethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &PortPreview{URL: resp.URL, Token: resp.Token}, nil
}

// GetComputerUseStatus returns the desktop service status.
func (s *Sandbox) GetComputerUseStatus() (*ComputerUseStatus, error) {
	client, err := s.toolboxClient()
	if err != nil {
		return nil, err
	}
	var resp ComputerUseStatus
	if err := client.doJSON(context.Background(), httpMethodGet, "/toolbox/computeruse/status", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StartComputerUse starts desktop services.
func (s *Sandbox) StartComputerUse() (*ComputerUseStartResponse, error) {
	client, err := s.toolboxClient()
	if err != nil {
		return nil, err
	}
	var resp ComputerUseStartResponse
	if err := client.doJSON(context.Background(), httpMethodPost, "/toolbox/computeruse/start", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StopComputerUse stops desktop services.
func (s *Sandbox) StopComputerUse() (*ComputerUseStopResponse, error) {
	client, err := s.toolboxClient()
	if err != nil {
		return nil, err
	}
	var resp ComputerUseStopResponse
	if err := client.doJSON(context.Background(), httpMethodPost, "/toolbox/computeruse/stop", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
