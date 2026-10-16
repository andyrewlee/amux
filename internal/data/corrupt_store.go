package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CorruptStoreError reports a refused store write: the existing file could
// not be decoded, and rather than silently discarding user-authored content
// the write was refused so the bytes could be preserved for manual
// recovery. Reads keep tolerating the file as empty — only writes refuse.
type CorruptStoreError struct {
	// Path is the store file that failed to decode.
	Path string
	// Preserved is the .bak path the corrupt bytes were moved to, or ""
	// when they were left in place (see BackupOccupied) or there was
	// nothing on disk to preserve.
	Preserved string
	// BackupOccupied reports that Path+".bak" already held an earlier
	// preservation: the corrupt primary was left in place rather than
	// destroying either file.
	BackupOccupied bool
	// Err is the underlying decode failure.
	Err error
}

func (e *CorruptStoreError) Error() string {
	switch {
	case e.Preserved != "":
		return fmt.Sprintf("%s is corrupt; original preserved at %s for manual recovery — save again to write: %v", filepath.Base(e.Path), e.Preserved, e.Err)
	case e.BackupOccupied:
		return fmt.Sprintf("%s is corrupt and its backup already exists; refusing to destroy either — resolve %s.bak manually: %v", filepath.Base(e.Path), e.Path, e.Err)
	default:
		return fmt.Sprintf("%s is corrupt; write refused: %v", filepath.Base(e.Path), e.Err)
	}
}

func (e *CorruptStoreError) Unwrap() error { return e.Err }

// isStoreDecodeError reports whether err is a JSON decode failure — the
// corruption class preserve-and-refuse covers. Schema-version refusals and
// I/O errors are deliberately not corruption: a newer-schema file holds
// valid data owned by a newer binary, and a read failure may be transient —
// neither should move or replace the file.
func isStoreDecodeError(err error) bool {
	var synErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &synErr) || errors.As(err, &typeErr)
}

// preserveCorruptPrimary moves a corrupt store file to its path+".bak"
// sibling so a refused write never silently discards the bytes. The first
// return is the backup path the file moved to; "" means it stayed in place,
// distinguished by the second return: true when an older .bak already
// occupies the slot (the earlier preservation wins — moving would destroy
// it), false when the primary was absent or could not be renamed (nothing
// on disk needed preserving).
//
// The .bak name is shared with fsatomic's Windows replace-fallback: a
// subsequent successful write may reclaim the slot there, so .bak is a
// best-effort quarantine, not a durable archive.
func preserveCorruptPrimary(path string) (preserved string, occupied bool) {
	backup := path + ".bak"
	if _, err := os.Stat(backup); err == nil {
		return "", true
	}
	if err := os.Rename(path, backup); err != nil {
		return "", false
	}
	return backup, false
}
