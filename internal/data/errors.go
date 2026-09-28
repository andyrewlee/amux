package data

import "errors"

// ErrUnsupportedSchemaVersion marks a store file whose schema version is
// newer than this binary understands. Read paths fail closed on it like any
// load failure, but write paths must refuse rather than overwrite the file —
// its data is valid for a newer binary, not corrupt.
var ErrUnsupportedSchemaVersion = errors.New("unsupported store schema version")
