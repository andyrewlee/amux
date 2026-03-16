package cli

import (
	"bytes"
	"os"
	"testing"
)

func TestPromptSecretReadsFromNonTTYInput(t *testing.T) {
	prevStdin := os.Stdin
	prevStdout := cliStdout
	defer func() {
		os.Stdin = prevStdin
		cliStdout = prevStdout
	}()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	if _, err := writer.WriteString(" secret-value \n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	_ = writer.Close()

	os.Stdin = reader
	var output bytes.Buffer
	cliStdout = &output

	got, err := promptSecret("Daytona API key: ")
	if err != nil {
		t.Fatalf("promptSecret() error = %v", err)
	}
	if got != "secret-value" {
		t.Fatalf("promptSecret() = %q, want %q", got, "secret-value")
	}
	if gotOutput := output.String(); gotOutput != "Daytona API key: " {
		t.Fatalf("prompt output = %q, want %q", gotOutput, "Daytona API key: ")
	}
}
