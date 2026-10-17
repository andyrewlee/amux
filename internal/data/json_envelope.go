package data

import (
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// projectEnvelope owns the shared mechanics of the per-project JSON-
// envelope stores (project-env.json, project-scripts.json): read →
// tolerate-missing → decode+schema-check → records map on the load side,
// and load → mutate → flock → atomic replace on the write side.
//
// What stays per-store (the parameters, not forks): the record type V, the
// envelope's wire shape and version check (decode/encode), what counts as
// an "absent" record, and whether records are copied across the boundary.
// The corrupt-file policy is NOT a parameter: every consumer shares the
// plan-110 preserve-and-refuse behavior because the user-authored-data
// contract is what makes these stores siblings in the first place.
type projectEnvelope[V any] struct {
	path string
	mu   sync.Mutex
	// readFile is a per-instance test seam; nil means os.ReadFile.
	readFile func(string) ([]byte, error)

	// decodeEnvelope unmarshals the versioned envelope into the records
	// map, refusing newer schemas. A store with a pre-versioning v0 shape
	// handles that fallback inside its decoder.
	decodeEnvelope func(raw []byte) (map[string]V, error)
	// encodeEnvelope wraps the records map in the versioned write envelope
	// for fsatomic.WriteJSON.
	encodeEnvelope func(all map[string]V) any
	// emptyEntry reports an "absent" record — an empty layer means
	// absence, not override-to-empty, so Set deletes the key.
	emptyEntry func(V) bool
	// copyRecord returns the record crossing a store boundary: a fresh
	// map for map-typed records so callers can't alias store state, nil
	// (identity) for value types.
	copyRecord func(V) V
}

// Path exposes the backing file path for diagnostics.
func (s *projectEnvelope[V]) Path() string { return s.path }

// load reads and decodes the store file. A missing file reads as empty
// state; every other outcome (corrupt, newer schema, I/O failure) returns
// the error unmodified so the caller's policy branch classifies it.
func (s *projectEnvelope[V]) load() (map[string]V, error) {
	read := os.ReadFile
	if s.readFile != nil {
		read = s.readFile
	}
	raw, err := read(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]V{}, nil
		}
		return nil, err
	}
	return s.decodeEnvelope(raw)
}

// forRepo returns the project's record (normalized key), or zero when the
// project has none or the file is unreadable — a corrupt or missing file
// must never block script execution.
func (s *projectEnvelope[V]) forRepo(repoPath string, zero V) V {
	key := NormalizePath(repoPath)
	if key == "" {
		return zero
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return zero
	}
	if s.copyRecord != nil {
		return s.copyRecord(all[key])
	}
	return all[key]
}

// set replaces the project's record and persists it atomically; an
// emptyEntry record removes the key. The sibling flock serializes the
// whole load→mutate→write against other amux processes — fsatomic prevents
// torn writes, not lost updates.
func (s *projectEnvelope[V]) set(repoPath string, rec V) error {
	key := NormalizePath(repoPath)
	if key == "" {
		return errors.New("project repo path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lockFile, err := lockRegistryFile(s.path+".lock", false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)
	all, err := s.load()
	if err != nil {
		if isStoreDecodeError(err) {
			// User-authored data: preserve the corrupt bytes beside the
			// store and refuse this write — zeroing the file would
			// silently drop every project's records at once.
			preserved, occupied := preserveCorruptPrimary(s.path)
			return &CorruptStoreError{Path: s.path, Preserved: preserved, BackupOccupied: occupied, Err: err}
		}
		// A newer schema or any non-decode read failure is existing
		// state this binary cannot inspect — refuse rather than
		// clobbering bytes that may hold other projects' records.
		return err
	}
	if s.emptyEntry(rec) {
		delete(all, key)
	} else if s.copyRecord != nil {
		all[key] = s.copyRecord(rec)
	} else {
		all[key] = rec
	}
	return fsatomic.WriteJSON(s.path, s.encodeEnvelope(all))
}
