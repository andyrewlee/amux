package messages

import (
	"errors"
	"testing"
)

func TestErrorFormatting(t *testing.T) {
	err := Error{Err: errors.New("boom"), Context: "context"}
	if err.Error() != "context: boom" {
		t.Fatalf("unexpected formatted error: %q", err.Error())
	}

	err = Error{Err: errors.New("boom")}
	if err.Error() != "boom" {
		t.Fatalf("unexpected formatted error without context: %q", err.Error())
	}
}
