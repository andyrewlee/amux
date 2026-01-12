package computer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// MockRemoteComputer provides a test double for RemoteComputer interface.
type MockRemoteComputer struct {
	mu sync.RWMutex

	id       string
	name     string
	state    ComputerState
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

// NewMockRemoteComputer creates a new mock computer with default settings.
func NewMockRemoteComputer(id string) *MockRemoteComputer {
	return &MockRemoteComputer{
		id:          id,
		name:        "mock-computer",
		state:       StateStarted,
		labels:      map[string]string{"amux.provider": "mock"},
		homeDir:     "/home/user",
		username:    "user",
		Files:       make(map[string]string),
		ExecResults: make(map[string]MockExecResult),
	}
}

func (m *MockRemoteComputer) ID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.id
}

func (m *MockRemoteComputer) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}

func (m *MockRemoteComputer) State() ComputerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *MockRemoteComputer) Labels() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range m.labels {
		result[k] = v
	}
	return result
}

func (m *MockRemoteComputer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = StateStarted
	return nil
}

func (m *MockRemoteComputer) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = StateStopped
	return nil
}

func (m *MockRemoteComputer) WaitReady(ctx context.Context, timeout time.Duration) error {
	return nil
}

func (m *MockRemoteComputer) Exec(ctx context.Context, cmd string, opts *ExecOptions) (*ExecResult, error) {
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

func (m *MockRemoteComputer) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error) {
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

func (m *MockRemoteComputer) UploadFile(ctx context.Context, src, dst string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.UploadHistory = append(m.UploadHistory, MockUpload{Source: src, Dest: dst})
	return nil
}

func (m *MockRemoteComputer) DownloadFile(ctx context.Context, src, dst string) error {
	return nil
}

func (m *MockRemoteComputer) GetPreviewURL(ctx context.Context, port int) (string, error) {
	return fmt.Sprintf("http://localhost:%d", port), nil
}

func (m *MockRemoteComputer) Refresh(ctx context.Context) error {
	return nil
}

// Additional helper methods not in interface

func (m *MockRemoteComputer) Username() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.username
}

func (m *MockRemoteComputer) HomeDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.homeDir
}

// Test helper methods

// SetState sets the computer state for testing.
func (m *MockRemoteComputer) SetState(state ComputerState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
}

// SetHomeDir sets the home directory for testing.
func (m *MockRemoteComputer) SetHomeDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.homeDir = dir
}

// SetExecResult sets the result for commands starting with the given prefix.
func (m *MockRemoteComputer) SetExecResult(cmdPrefix string, output string, exitCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecResults[cmdPrefix] = MockExecResult{Output: output, ExitCode: exitCode}
}

// SetExecError sets an error for commands starting with the given prefix.
func (m *MockRemoteComputer) SetExecError(cmdPrefix string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecResults[cmdPrefix] = MockExecResult{Error: err}
}

// SetFile sets a file in the mock filesystem.
func (m *MockRemoteComputer) SetFile(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[path] = content
}

// GetExecHistory returns all executed commands.
func (m *MockRemoteComputer) GetExecHistory() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.ExecHistory))
	copy(result, m.ExecHistory)
	return result
}

// GetUploadHistory returns all uploaded files.
func (m *MockRemoteComputer) GetUploadHistory() []MockUpload {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]MockUpload, len(m.UploadHistory))
	copy(result, m.UploadHistory)
	return result
}

// ClearHistory clears the exec and upload history.
func (m *MockRemoteComputer) ClearHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecHistory = nil
	m.UploadHistory = nil
}
