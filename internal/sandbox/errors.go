package sandbox

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCode represents specific error categories for better handling.
type ErrorCode string

const (
	ErrCodeUnknown        ErrorCode = "unknown"
	ErrCodeSandboxCreate  ErrorCode = "sandbox_create"
	ErrCodeSandboxStart   ErrorCode = "sandbox_start"
	ErrCodeSandboxMissing ErrorCode = "sandbox_not_found"
	ErrCodeCredentials    ErrorCode = "credentials"
	ErrCodeSync           ErrorCode = "sync"
	ErrCodeAgent          ErrorCode = "agent"
	ErrCodeSSH            ErrorCode = "ssh"
	ErrCodeNetwork        ErrorCode = "network"
	ErrCodeConfig         ErrorCode = "config"
	ErrCodeVolume         ErrorCode = "volume"
	ErrCodeSnapshot       ErrorCode = "snapshot"
	ErrCodePreflight      ErrorCode = "preflight"
	ErrCodeTimeout        ErrorCode = "timeout"
	ErrCodePermission     ErrorCode = "permission"
)

// ErrNotFound is a sentinel error for resource-not-found cases.
var ErrNotFound = errors.New("not found")

// SandboxError is a structured error type with context for debugging.
type SandboxError struct {
	Code       ErrorCode
	Op         string            // Operation that failed (e.g., "create", "sync", "credentials.setup")
	Agent      Agent             // Agent involved, if applicable
	SandboxID  string            // Sandbox ID, if known
	Cause      error             // Underlying error
	Context    map[string]string // Additional context
	Suggestion string            // User-friendly suggestion for resolution
	Retryable  bool              // Whether the operation can be retried
}

// Error implements the error interface.
func (e *SandboxError) Error() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] %s failed", e.Code, e.Op))
	if e.Agent != "" {
		b.WriteString(fmt.Sprintf(" (agent: %s)", e.Agent))
	}
	if e.SandboxID != "" {
		b.WriteString(fmt.Sprintf(" (sandbox: %s)", truncateID(e.SandboxID)))
	}
	if e.Cause != nil {
		b.WriteString(fmt.Sprintf(": %v", e.Cause))
	}
	return b.String()
}

// Unwrap returns the underlying error.
func (e *SandboxError) Unwrap() error {
	return e.Cause
}

// UserMessage returns a human-friendly error message with suggestions.
func (e *SandboxError) UserMessage() string {
	var b strings.Builder

	// Start with a clear description
	switch e.Code {
	case ErrCodeSandboxCreate:
		b.WriteString("Failed to create sandbox")
	case ErrCodeSandboxStart:
		b.WriteString("Failed to start sandbox")
	case ErrCodeSandboxMissing:
		b.WriteString("Sandbox not found")
	case ErrCodeCredentials:
		b.WriteString("Credential setup failed")
	case ErrCodeSync:
		b.WriteString("Workspace sync failed")
	case ErrCodeAgent:
		b.WriteString("Agent operation failed")
	case ErrCodeSSH:
		b.WriteString("SSH connection failed")
	case ErrCodeNetwork:
		b.WriteString("Network error")
	case ErrCodeVolume:
		b.WriteString("Volume operation failed")
	case ErrCodeSnapshot:
		b.WriteString("Snapshot operation failed")
	case ErrCodePreflight:
		b.WriteString("Preflight check failed")
	case ErrCodeTimeout:
		b.WriteString("Operation timed out")
	case ErrCodePermission:
		b.WriteString("Permission denied")
	default:
		b.WriteString("An error occurred")
	}

	if e.Agent != "" {
		b.WriteString(fmt.Sprintf(" for %s", e.Agent))
	}

	// Add cause if available
	if e.Cause != nil {
		b.WriteString(fmt.Sprintf("\n\nDetails: %v", e.Cause))
	}

	// Add suggestion if available
	if e.Suggestion != "" {
		b.WriteString("\n\nSuggestion: " + e.Suggestion)
	} else {
		// Generate default suggestions based on error code
		suggestion := e.defaultSuggestion()
		if suggestion != "" {
			b.WriteString("\n\nSuggestion: " + suggestion)
		}
	}

	return b.String()
}

func (e *SandboxError) defaultSuggestion() string {
	switch e.Code {
	case ErrCodeSandboxCreate:
		return "Check your provider credentials with `amux auth status`. Run `amux doctor` for diagnostics."
	case ErrCodeSandboxMissing:
		return "The sandbox may have been deleted. Run `amux sandbox ls` to see available sandboxes."
	case ErrCodeCredentials:
		return "Try running `amux doctor --deep` to diagnose credential issues."
	case ErrCodeSync:
		return "Check available disk space and network connectivity. Try `amux sandbox run --no-sync` to skip sync."
	case ErrCodeSSH:
		return "Ensure SSH is installed and accessible. Check firewall settings. Run `amux doctor` for diagnostics."
	case ErrCodeNetwork:
		return "Check your internet connection and firewall settings."
	case ErrCodeVolume:
		return "Volume operation failed. Run `amux doctor --deep` to check."
	case ErrCodeTimeout:
		if e.Retryable {
			return "The operation timed out but may succeed if retried."
		}
		return "The operation timed out. Check network connectivity and try again."
	case ErrCodePreflight:
		return "Run `amux doctor` to identify and fix the issue."
	default:
		return ""
	}
}

// Helper functions for creating common errors

// NewSandboxError creates a new SandboxError with the given parameters.
func NewSandboxError(code ErrorCode, op string, cause error) *SandboxError {
	return &SandboxError{
		Code:    code,
		Op:      op,
		Cause:   cause,
		Context: make(map[string]string),
	}
}

// WithAgent adds agent context to the error.
func (e *SandboxError) WithAgent(agent Agent) *SandboxError {
	e.Agent = agent
	return e
}

// WithSandbox adds sandbox ID context to the error.
func (e *SandboxError) WithSandbox(id string) *SandboxError {
	e.SandboxID = id
	return e
}

// WithContext adds additional context to the error.
func (e *SandboxError) WithContext(key, value string) *SandboxError {
	if e.Context == nil {
		e.Context = make(map[string]string)
	}
	e.Context[key] = value
	return e
}

// WithSuggestion sets a user-friendly suggestion.
func (e *SandboxError) WithSuggestion(suggestion string) *SandboxError {
	e.Suggestion = suggestion
	return e
}

// WithRetryable marks the error as retryable.
func (e *SandboxError) WithRetryable(retryable bool) *SandboxError {
	e.Retryable = retryable
	return e
}

// Convenience constructors for common errors

func ErrCredentialSetup(op string, cause error) *SandboxError {
	return NewSandboxError(ErrCodeCredentials, op, cause)
}

func ErrSyncFailed(op string, cause error) *SandboxError {
	return NewSandboxError(ErrCodeSync, op, cause)
}

func ErrAgentInstall(agent Agent, cause error) *SandboxError {
	return NewSandboxError(ErrCodeAgent, "install", cause).WithAgent(agent)
}

func ErrSSHConnection(cause error) *SandboxError {
	return NewSandboxError(ErrCodeSSH, "connect", cause).WithRetryable(true)
}

func ErrTimeout(op string) *SandboxError {
	return NewSandboxError(ErrCodeTimeout, op, errors.New("operation timed out")).WithRetryable(true)
}

func ErrVolumeNotReady(volumeName string, cause error) *SandboxError {
	return NewSandboxError(ErrCodeVolume, "wait_ready", cause).
		WithContext("volume", volumeName)
}

// IsSandboxError checks if an error is a SandboxError.
func IsSandboxError(err error) bool {
	var se *SandboxError
	return errors.As(err, &se)
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	if se := GetSandboxError(err); se != nil {
		return se.Retryable
	}
	return false
}

func truncateID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// GetSandboxError extracts a SandboxError from an error chain if present.
func GetSandboxError(err error) *SandboxError {
	if err == nil {
		return nil
	}
	var se *SandboxError
	if errors.As(err, &se) {
		return se
	}
	return nil
}

// IsNotFoundError checks if error represents a "not found" condition.
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	if se := GetSandboxError(err); se != nil {
		return se.Code == ErrCodeSandboxMissing
	}
	return false
}

// MultiError collects multiple errors.
type MultiError struct {
	Errors []error
}

func (m *MultiError) Error() string {
	if len(m.Errors) == 0 {
		return "no errors"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d errors occurred:\n", len(m.Errors)))
	for i, err := range m.Errors {
		b.WriteString(fmt.Sprintf("  %d. %v\n", i+1, err))
	}
	return b.String()
}

func (m *MultiError) Add(err error) {
	if err != nil {
		m.Errors = append(m.Errors, err)
	}
}

func (m *MultiError) HasErrors() bool {
	return len(m.Errors) > 0
}

func (m *MultiError) ErrorOrNil() error {
	if m.HasErrors() {
		return m
	}
	return nil
}
