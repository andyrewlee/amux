package sandbox

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// MockRemoteSandbox provides a test double for RemoteSandbox interface.
type MockRemoteSandbox struct {
	mu sync.RWMutex

	id       string
	name     string
	state    SandboxState
	labels   map[string]string
	homeDir  string
	username string

	// Files simulates the filesystem
	Files map[string]string

	// ExecResults maps command prefixes to (output, error) pairs
	ExecResults map[string]MockExecResult

	// ExecHistory records all executed commands
	ExecHistory []string

	// UploadHistory records all uploaded files
	UploadHistory []MockUpload
}

// MockExecResult represents the result of a command execution.
type MockExecResult struct {
	Output   string
	ExitCode int
	Error    error
}

// MockUpload represents a file upload operation.
type MockUpload struct {
	Source string
	Dest   string
}

// NewMockRemoteSandbox creates a new mock sandbox with default settings.
func NewMockRemoteSandbox(id string) *MockRemoteSandbox {
	return &MockRemoteSandbox{
		id:          id,
		name:        "mock-sandbox",
		state:       StateStarted,
		labels:      map[string]string{"amux.provider": "mock"},
		homeDir:     "/home/user",
		username:    "user",
		Files:       make(map[string]string),
		ExecResults: make(map[string]MockExecResult),
	}
}

func (m *MockRemoteSandbox) ID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.id
}

func (m *MockRemoteSandbox) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}

func (m *MockRemoteSandbox) State() SandboxState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *MockRemoteSandbox) Labels() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range m.labels {
		result[k] = v
	}
	return result
}

func (m *MockRemoteSandbox) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = StateStarted
	return nil
}

func (m *MockRemoteSandbox) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = StateStopped
	return nil
}

func (m *MockRemoteSandbox) WaitReady(ctx context.Context, timeout time.Duration) error {
	return nil
}

func (m *MockRemoteSandbox) Exec(ctx context.Context, cmd string, opts *ExecOptions) (*ExecResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ExecHistory = append(m.ExecHistory, cmd)

	// Check for matching exec results
	for prefix, result := range m.ExecResults {
		if strings.HasPrefix(cmd, prefix) {
			if result.Error != nil {
				return nil, result.Error
			}
			return &ExecResult{
				Stdout:   result.Output,
				ExitCode: result.ExitCode,
			}, nil
		}
	}

	// Default: command succeeded with empty output
	return &ExecResult{Stdout: "", ExitCode: 0}, nil
}

func (m *MockRemoteSandbox) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error) {
	m.mu.Lock()
	m.ExecHistory = append(m.ExecHistory, cmd)
	m.mu.Unlock()

	// Check for matching exec results
	m.mu.RLock()
	for prefix, result := range m.ExecResults {
		if strings.HasPrefix(cmd, prefix) {
			if result.Error != nil {
				m.mu.RUnlock()
				return 1, result.Error
			}
			if stdout != nil && result.Output != "" {
				stdout.Write([]byte(result.Output))
			}
			m.mu.RUnlock()
			return result.ExitCode, nil
		}
	}
	m.mu.RUnlock()

	return 0, nil
}

func (m *MockRemoteSandbox) UploadFile(ctx context.Context, src, dst string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.UploadHistory = append(m.UploadHistory, MockUpload{Source: src, Dest: dst})
	return nil
}

func (m *MockRemoteSandbox) DownloadFile(ctx context.Context, src, dst string) error {
	return nil
}

func (m *MockRemoteSandbox) GetPreviewURL(ctx context.Context, port int) (string, error) {
	return fmt.Sprintf("http://localhost:%d", port), nil
}

func (m *MockRemoteSandbox) Refresh(ctx context.Context) error {
	return nil
}

// Additional helper methods not in interface

func (m *MockRemoteSandbox) Username() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.username
}

func (m *MockRemoteSandbox) HomeDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.homeDir
}

// Test helper methods

// SetState sets the sandbox state for testing.
func (m *MockRemoteSandbox) SetState(state SandboxState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
}

// SetHomeDir sets the home directory for testing.
func (m *MockRemoteSandbox) SetHomeDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.homeDir = dir
}

// SetExecResult sets the result for commands starting with the given prefix.
func (m *MockRemoteSandbox) SetExecResult(cmdPrefix, output string, exitCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecResults[cmdPrefix] = MockExecResult{Output: output, ExitCode: exitCode}
}

// SetExecError sets an error for commands starting with the given prefix.
func (m *MockRemoteSandbox) SetExecError(cmdPrefix string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecResults[cmdPrefix] = MockExecResult{Error: err}
}

// SetFile sets a file in the mock filesystem.
func (m *MockRemoteSandbox) SetFile(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[path] = content
}

// GetExecHistory returns all executed commands.
func (m *MockRemoteSandbox) GetExecHistory() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.ExecHistory))
	copy(result, m.ExecHistory)
	return result
}

// GetUploadHistory returns all uploaded files.
func (m *MockRemoteSandbox) GetUploadHistory() []MockUpload {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]MockUpload, len(m.UploadHistory))
	copy(result, m.UploadHistory)
	return result
}

// ClearHistory clears the exec and upload history.
func (m *MockRemoteSandbox) ClearHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecHistory = nil
	m.UploadHistory = nil
}
