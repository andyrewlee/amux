package sandbox

import (
	"errors"
	"testing"
)

func TestSandboxError(t *testing.T) {
	t.Run("basic error creation", func(t *testing.T) {
		err := NewSandboxError(ErrCodeSSH, "connect", nil)
		if err.Code != ErrCodeSSH {
			t.Errorf("Expected code %s, got %s", ErrCodeSSH, err.Code)
		}
		if err.Op != "connect" {
			t.Errorf("Expected op 'connect', got %s", err.Op)
		}
	})

	t.Run("error with cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := NewSandboxError(ErrCodeNetwork, "dial", cause)
		if err.Cause != cause {
			t.Error("Expected cause to be set")
		}
		if !errors.Is(err, cause) {
			t.Error("errors.Is should return true for the cause")
		}
	})

	t.Run("error with suggestion", func(t *testing.T) {
		err := NewSandboxError(ErrCodeCredentials, "setup", nil).
			WithSuggestion("Run `amux setup` to configure credentials")
		if err.Suggestion == "" {
			t.Error("Expected suggestion to be set")
		}
	})

	t.Run("retryable error", func(t *testing.T) {
		err := NewSandboxError(ErrCodeSSH, "connect", nil).WithRetryable(true)
		if !err.Retryable {
			t.Error("Expected error to be retryable")
		}
	})

	t.Run("error message formatting", func(t *testing.T) {
		err := NewSandboxError(ErrCodeSync, "upload", errors.New("timeout"))
		msg := err.Error()
		if msg == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("user-friendly message", func(t *testing.T) {
		err := NewSandboxError(ErrCodeCredentials, "setup", nil)
		msg := err.UserMessage()
		if msg == "" {
			t.Error("Expected non-empty user message")
		}
	})
}

func TestErrorCodes(t *testing.T) {
	codes := []ErrorCode{
		ErrCodeCredentials,
		ErrCodeSync,
		ErrCodeSSH,
		ErrCodeNetwork,
		ErrCodeAgent,
		ErrCodePreflight,
		ErrCodeConfig,
		ErrCodeSnapshot,
	}

	for _, code := range codes {
		err := NewSandboxError(code, "test", nil)
		if err.Code != code {
			t.Errorf("Expected code %s, got %s", code, err.Code)
		}
	}
}

func TestConvenienceFunctions(t *testing.T) {
	t.Run("ErrCredentialSetup", func(t *testing.T) {
		err := ErrCredentialSetup("claude", errors.New("not found"))
		if err.Code != ErrCodeCredentials {
			t.Errorf("Expected code %s, got %s", ErrCodeCredentials, err.Code)
		}
	})

	t.Run("ErrSyncFailed", func(t *testing.T) {
		err := ErrSyncFailed("upload", errors.New("timeout"))
		if err.Code != ErrCodeSync {
			t.Errorf("Expected code %s, got %s", ErrCodeSync, err.Code)
		}
	})

	t.Run("ErrSSHConnection", func(t *testing.T) {
		err := ErrSSHConnection(errors.New("refused"))
		if err.Code != ErrCodeSSH {
			t.Errorf("Expected code %s, got %s", ErrCodeSSH, err.Code)
		}
		// SSH connection errors should be retryable by default
		if !err.Retryable {
			t.Error("Expected SSH connection error to be retryable")
		}
	})
}

func TestIsRetryable(t *testing.T) {
	t.Run("retryable sandbox error", func(t *testing.T) {
		err := NewSandboxError(ErrCodeSSH, "connect", nil).WithRetryable(true)
		if !IsRetryable(err) {
			t.Error("Expected IsRetryable to return true")
		}
	})

	t.Run("non-retryable sandbox error", func(t *testing.T) {
		err := NewSandboxError(ErrCodeConfig, "load", nil)
		if IsRetryable(err) {
			t.Error("Expected IsRetryable to return false")
		}
	})

	t.Run("non-sandbox error", func(t *testing.T) {
		err := errors.New("random error")
		if IsRetryable(err) {
			t.Error("Expected IsRetryable to return false for non-sandbox errors")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		if IsRetryable(nil) {
			t.Error("Expected IsRetryable to return false for nil")
		}
	})
}

func TestMultiError(t *testing.T) {
	t.Run("empty multi error", func(t *testing.T) {
		me := &MultiError{}
		if me.HasErrors() {
			t.Error("Expected HasErrors to return false for empty MultiError")
		}
		if me.ErrorOrNil() != nil {
			t.Error("Expected ErrorOrNil to return nil for empty MultiError")
		}
	})

	t.Run("multi error with errors", func(t *testing.T) {
		me := &MultiError{}
		me.Add(errors.New("error 1"))
		me.Add(errors.New("error 2"))
		if !me.HasErrors() {
			t.Error("Expected HasErrors to return true")
		}
		if me.ErrorOrNil() == nil {
			t.Error("Expected ErrorOrNil to return error")
		}
		if len(me.Errors) != 2 {
			t.Errorf("Expected 2 errors, got %d", len(me.Errors))
		}
	})

	t.Run("multi error ignores nil", func(t *testing.T) {
		me := &MultiError{}
		me.Add(nil)
		me.Add(errors.New("error"))
		me.Add(nil)
		if len(me.Errors) != 1 {
			t.Errorf("Expected 1 error, got %d", len(me.Errors))
		}
	})
}
