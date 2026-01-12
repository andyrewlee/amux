package computer

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCode represents specific error categories for better handling.
type ErrorCode string

const (
	ErrCodeUnknown         ErrorCode = "unknown"
	ErrCodeComputerCreate  ErrorCode = "computer_create"
	ErrCodeComputerStart   ErrorCode = "computer_start"
	ErrCodeComputerMissing ErrorCode = "computer_not_found"
	ErrCodeCredentials     ErrorCode = "credentials"
	ErrCodeSync            ErrorCode = "sync"
	ErrCodeAgent           ErrorCode = "agent"
	ErrCodeSSH             ErrorCode = "ssh"
	ErrCodeNetwork         ErrorCode = "network"
	ErrCodeConfig          ErrorCode = "config"
	ErrCodeVolume          ErrorCode = "volume"
	ErrCodeSnapshot        ErrorCode = "snapshot"
	ErrCodePreflight       ErrorCode = "preflight"
	ErrCodeTimeout         ErrorCode = "timeout"
	ErrCodePermission      ErrorCode = "permission"
)

// ComputerError is a structured error type with context for debugging.
type ComputerError struct {
	Code       ErrorCode
	Op         string            // Operation that failed (e.g., "create", "sync", "credentials.setup")
	Agent      Agent             // Agent involved, if applicable
	ComputerID string            // Computer ID, if known
	Cause      error             // Underlying error
	Context    map[string]string // Additional context
	Suggestion string            // User-friendly suggestion for resolution
	Retryable  bool              // Whether the operation can be retried
}

// Error implements the error interface.
func (e *ComputerError) Error() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] %s failed", e.Code, e.Op))
	if e.Agent != "" {
		b.WriteString(fmt.Sprintf(" (agent: %s)", e.Agent))
	}
	if e.ComputerID != "" {
		b.WriteString(fmt.Sprintf(" (computer: %s)", truncateID(e.ComputerID)))
	}
	if e.Cause != nil {
		b.WriteString(fmt.Sprintf(": %v", e.Cause))
	}
	return b.String()
}

// Unwrap returns the underlying error.
func (e *ComputerError) Unwrap() error {
	return e.Cause
}

// UserMessage returns a human-friendly error message with suggestions.
func (e *ComputerError) UserMessage() string {
	var b strings.Builder

	// Start with a clear description
	switch e.Code {
	case ErrCodeComputerCreate:
		b.WriteString("Failed to create computer")
	case ErrCodeComputerStart:
		b.WriteString("Failed to start computer")
	case ErrCodeComputerMissing:
		b.WriteString("Computer not found")
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
		b.WriteString(fmt.Sprintf("\n\nSuggestion: %s", e.Suggestion))
	} else {
		// Generate default suggestions based on error code
		suggestion := e.defaultSuggestion()
		if suggestion != "" {
			b.WriteString(fmt.Sprintf("\n\nSuggestion: %s", suggestion))
		}
	}

	return b.String()
}

func (e *ComputerError) defaultSuggestion() string {
	switch e.Code {
	case ErrCodeComputerCreate:
		return "Check your provider credentials with `amux auth status`. Run `amux doctor` for diagnostics."
	case ErrCodeComputerMissing:
		return "The computer may have been deleted. Run `amux computer ls` to see available computers."
	case ErrCodeCredentials:
		return "Try running `amux doctor --deep` to diagnose credential issues."
	case ErrCodeSync:
		return "Check available disk space and network connectivity. Try `amux computer run --no-sync` to skip sync."
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

// NewComputerError creates a new ComputerError with the given parameters.
func NewComputerError(code ErrorCode, op string, cause error) *ComputerError {
	return &ComputerError{
		Code:    code,
		Op:      op,
		Cause:   cause,
		Context: make(map[string]string),
	}
}

// WithAgent adds agent context to the error.
func (e *ComputerError) WithAgent(agent Agent) *ComputerError {
	e.Agent = agent
	return e
}

// WithComputer adds computer ID context to the error.
func (e *ComputerError) WithComputer(id string) *ComputerError {
	e.ComputerID = id
	return e
}

// WithContext adds additional context to the error.
func (e *ComputerError) WithContext(key, value string) *ComputerError {
	if e.Context == nil {
		e.Context = make(map[string]string)
	}
	e.Context[key] = value
	return e
}

// WithSuggestion adds a user-friendly suggestion.
func (e *ComputerError) WithSuggestion(suggestion string) *ComputerError {
	e.Suggestion = suggestion
	return e
}

// WithRetryable marks the error as retryable.
func (e *ComputerError) WithRetryable(retryable bool) *ComputerError {
	e.Retryable = retryable
	return e
}

// Convenience constructors for common errors

func ErrCredentialSetup(op string, cause error) *ComputerError {
	return NewComputerError(ErrCodeCredentials, op, cause)
}

func ErrSyncFailed(op string, cause error) *ComputerError {
	return NewComputerError(ErrCodeSync, op, cause)
}

func ErrAgentInstall(agent Agent, cause error) *ComputerError {
	return NewComputerError(ErrCodeAgent, "install", cause).WithAgent(agent)
}

func ErrSSHConnection(cause error) *ComputerError {
	return NewComputerError(ErrCodeSSH, "connect", cause).WithRetryable(true)
}

func ErrTimeout(op string) *ComputerError {
	return NewComputerError(ErrCodeTimeout, op, errors.New("operation timed out")).WithRetryable(true)
}

func ErrVolumeNotReady(volumeName string, cause error) *ComputerError {
	return NewComputerError(ErrCodeVolume, "wait_ready", cause).
		WithContext("volume", volumeName)
}

// IsComputerError checks if an error is a ComputerError.
func IsComputerError(err error) bool {
	var se *ComputerError
	return errors.As(err, &se)
}

// GetComputerError extracts a ComputerError from an error chain.
func GetComputerError(err error) *ComputerError {
	var se *ComputerError
	if errors.As(err, &se) {
		return se
	}
	return nil
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	if se := GetComputerError(err); se != nil {
		return se.Retryable
	}
	return false
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
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
