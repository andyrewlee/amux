package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// Envelope wraps all --json responses.
type Envelope struct {
	OK            bool       `json:"ok"`
	Data          any        `json:"data"`
	Error         *ErrorInfo `json:"error"`
	Meta          Meta       `json:"meta"`
	SchemaVersion string     `json:"schema_version"`
	RequestID     string     `json:"request_id,omitempty"`
	Command       string     `json:"command,omitempty"`
}

// ErrorInfo describes an error in the JSON envelope.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Meta contains response metadata.
type Meta struct {
	GeneratedAt string `json:"generated_at"`
	AmuxVersion string `json:"amux_version"`
}

const EnvelopeSchemaVersion = "amux.cli.v1"

type responseContext struct {
	mu        sync.RWMutex
	requestID string
	command   string
}

var cliResponseContext responseContext

// Exit codes.
const (
	ExitOK            = 0
	ExitUsage         = 2
	ExitNotFound      = 3
	ExitDependency    = 4
	ExitUnsafeBlocked = 5
	ExitInternalError = 1
)

func newMeta(version string) Meta {
	return Meta{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		AmuxVersion: version,
	}
}

func setResponseContext(requestID, command string) {
	cliResponseContext.mu.Lock()
	defer cliResponseContext.mu.Unlock()
	cliResponseContext.requestID = requestID
	cliResponseContext.command = command
}

func clearResponseContext() {
	setResponseContext("", "")
}

func currentResponseContext() (string, string) {
	cliResponseContext.mu.RLock()
	defer cliResponseContext.mu.RUnlock()
	return cliResponseContext.requestID, cliResponseContext.command
}

func successEnvelope(data any, version string) Envelope {
	requestID, command := currentResponseContext()
	return Envelope{
		OK:            true,
		Data:          data,
		Meta:          newMeta(version),
		SchemaVersion: EnvelopeSchemaVersion,
		RequestID:     requestID,
		Command:       command,
	}
}

func errorEnvelope(code, message string, details any, version string) Envelope {
	requestID, command := currentResponseContext()
	return Envelope{
		OK: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Details: details,
		},
		Meta:          newMeta(version),
		SchemaVersion: EnvelopeSchemaVersion,
		RequestID:     requestID,
		Command:       command,
	}
}

func encodeEnvelope(env Envelope) ([]byte, error) {
	return json.MarshalIndent(env, "", "  ")
}

func writeEnvelope(w io.Writer, env Envelope) {
	data, err := encodeEnvelope(env)
	if err != nil {
		// Best effort fallback keeps JSON contract intact.
		fallback := []byte(`{"ok":false,"error":{"code":"encode_failed","message":"failed to encode response"},"data":null}` + "\n")
		_, _ = w.Write(fallback)
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

// PrintJSON writes a success envelope to w.
func PrintJSON(w io.Writer, data any, version string) {
	writeEnvelope(w, successEnvelope(data, version))
}

// ReturnError writes an error envelope to w.
func ReturnError(w io.Writer, code, message string, details any, version string) {
	writeEnvelope(w, errorEnvelope(code, message, details, version))
}

// PrintHuman calls fn to produce human-readable output.
func PrintHuman(w io.Writer, fn func(io.Writer)) {
	fn(w)
}

// Errorf prints a human-readable error to w.
func Errorf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "Error: "+format+"\n", args...)
}
