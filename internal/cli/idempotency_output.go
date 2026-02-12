package cli

import (
	"io"
	"strings"
)

func writeJSONEnvelopeWithIdempotency(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	command string,
	key string,
	exitCode int,
	env Envelope,
) int {
	_ = wErr
	encoded, err := encodeEnvelope(env)
	if err != nil {
		ReturnError(w, "encode_failed", "failed to encode response", nil, version)
		return ExitInternalError
	}
	encoded = append(encoded, '\n')

	key = strings.TrimSpace(key)
	if key != "" {
		if !gf.JSON {
			_, _ = w.Write(encoded)
			return exitCode
		}
		// Persist first so mutating JSON calls fail closed when idempotency state
		// cannot be recorded.
		store, storeErr := newIdempotencyStore()
		if storeErr != nil {
			ReturnError(w, "idempotency_failed", storeErr.Error(), nil, version)
			return ExitInternalError
		}
		if storeErr := store.store(command, key, exitCode, encoded); storeErr != nil {
			ReturnError(w, "idempotency_failed", storeErr.Error(), nil, version)
			return ExitInternalError
		}
	}
	_, _ = w.Write(encoded)
	return exitCode
}

func returnJSONSuccessWithIdempotency(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	command string,
	key string,
	data any,
) int {
	return writeJSONEnvelopeWithIdempotency(
		w,
		wErr,
		gf,
		version,
		command,
		key,
		ExitOK,
		successEnvelope(data, version),
	)
}

func returnJSONErrorWithIdempotency(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	command string,
	key string,
	exitCode int,
	errorCode string,
	message string,
	details any,
) int {
	return writeJSONEnvelopeWithIdempotency(
		w,
		wErr,
		gf,
		version,
		command,
		key,
		exitCode,
		errorEnvelope(errorCode, message, details, version),
	)
}

func returnJSONErrorMaybeIdempotent(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	command string,
	key string,
	exitCode int,
	errorCode string,
	message string,
	details any,
) int {
	if !gf.JSON {
		_ = wErr
		return exitCode
	}
	if strings.TrimSpace(key) == "" {
		ReturnError(w, errorCode, message, details, version)
		return exitCode
	}
	return returnJSONErrorWithIdempotency(
		w,
		wErr,
		gf,
		version,
		command,
		key,
		exitCode,
		errorCode,
		message,
		details,
	)
}
