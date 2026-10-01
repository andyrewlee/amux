package data

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unicode"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// Durable port reservations: one JSON registry per amux state home, shared by
// every app instance pointed at that home. A reservation maps a workspace's
// persisted metadata ID to an inclusive port interval and is deliberately
// never reclaimed — retention is the first-version contract, so a stale
// release signal from a crashed or restarted app can never hand a live
// session's range to another workspace. The registry file outlives every
// consumer; there is no TTL, socket probing, or owner lease.
//
// All reads and read-modify-writes run under the registry flock conventions
// (registry_lock_*.go), so concurrent app instances serialize whole
// transactions rather than racing last-writer-wins file updates. Writes go
// through fsatomic (temp + fsync + rename), so a crash mid-write leaves the
// previous committed envelope intact.
type PortReservationStore struct {
	path     string
	lockPath string

	// guardMu covers guard only; the registry lock covers the file.
	guardMu sync.Mutex
	// guard is the first-adoption check injected by Initialize. It runs only
	// while creating a missing registry — never on the read path — and its
	// failure leaves no file behind. Stored so a Reserve that finds the file
	// missing applies the same guarded initialization instead of silently
	// minting a fresh registry over live legacy sessions.
	guard func() error
}

// portReservationsFileVersion is the only schema version this binary reads or
// writes. A newer version is intact data from a newer binary — it fails closed
// rather than being rewritten or ignored.
const portReservationsFileVersion = 1

// PortReservationRegistryFile is the registry filename under Paths.Home.
const PortReservationRegistryFile = "port-reservations.json"

// PortReservationInterval is one inclusive [start, end] port range.
type PortReservationInterval struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Snapshot returns a locked read-only copy of the reservation map —
// workspace metadata ID → interval. A missing registry reports an empty
// non-nil map (reads never create state); corrupt data still fails closed.
// Intended for diagnostics (e.g. counting reservations whose owner
// workspace is gone): the returned map is a copy, so mutating it cannot
// touch the registry.
func (s *PortReservationStore) Snapshot() (map[string]PortReservationInterval, error) {
	out := map[string]PortReservationInterval{}
	err := s.withLock(true, func() error {
		file, _, err := s.readLocked()
		if err != nil {
			return err
		}
		if file == nil {
			return nil
		}
		for id, iv := range file.Reservations {
			out[id] = iv
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// portReservationFile is the on-disk envelope.
type portReservationFile struct {
	Version      int                                `json:"version"`
	Reservations map[string]PortReservationInterval `json:"reservations"`
}

var (
	// ErrPortReservationsInvalid marks registry content that failed
	// full-envelope validation — corrupt, truncated, null, overlapping,
	// out-of-range, or ambiguous data. The bytes are always left untouched.
	ErrPortReservationsInvalid = errors.New("invalid port reservations registry")
	// ErrPortReservationsExhausted reports that no configured-size interval
	// starting at the configured base remains non-overlapping with every
	// persisted reservation. The process layer surfaces it as
	// ErrPortRangeExhausted through spawn error handling.
	ErrPortReservationsExhausted = errors.New("port reservations exhausted")
	// ErrPortReservationRangeInvalid reports a configured start/size that can
	// never produce a legal interval (non-positive width, out-of-range base,
	// or an end that would exceed 65535).
	ErrPortReservationRangeInvalid = errors.New("invalid port reservation range")
)

// NewPortReservationStore returns the store for the registry under the amux
// state home (Paths.Home). Construction performs no I/O; Initialize performs
// guarded first-adoption before any reservation is served.
func NewPortReservationStore(homeDir string) *PortReservationStore {
	return &PortReservationStore{
		path:     filepath.Join(homeDir, PortReservationRegistryFile),
		lockPath: filepath.Join(homeDir, PortReservationRegistryFile+".lock"),
	}
}

// Path returns the registry file path — exposed for diagnostics and tests.
func (s *PortReservationStore) Path() string {
	return s.path
}

// Initialize validates the existing registry or performs guarded first
// adoption when it is missing. guard runs exactly once per missing-file
// observation, before the empty envelope is written, and is the hook the app
// layer uses to refuse first adoption while legacy amux sessions survive. An
// existing registry is validated in full; corrupt or newer-schema bytes are
// an error and are never rewritten. A nil guard accepts creation
// unconditionally (isolated tests); production always injects one.
func (s *PortReservationStore) Initialize(guard func() error) error {
	s.guardMu.Lock()
	s.guard = guard
	s.guardMu.Unlock()
	return s.withLock(false, func() error {
		_, err := s.loadOrInitializeLocked()
		return err
	})
}

// Reserve returns the workspace's persisted inclusive interval, allocating and
// committing a new one when the ID has none. Re-reserving an existing ID is
// idempotent and returns the stored interval verbatim — even when its width or
// position no longer matches the current configuration. New IDs take the first
// configured-size interval at the configured base that does not overlap ANY
// persisted interval, including ones minted under older settings.
func (s *PortReservationStore) Reserve(workspaceID string, start, size int) (base, end int, err error) {
	if !validReservationID(workspaceID) {
		return 0, 0, fmt.Errorf("%w: workspace ID %q is not a valid reservation key", ErrPortReservationsInvalid, workspaceID)
	}
	if !validReservationRange(start, size) {
		return 0, 0, fmt.Errorf("%w: start=%d size=%d cannot form an interval inside 1-65535", ErrPortReservationRangeInvalid, start, size)
	}
	err = s.withLock(false, func() error {
		file, err := s.loadOrInitializeLocked()
		if err != nil {
			return err
		}
		if iv, ok := file.Reservations[workspaceID]; ok {
			base, end = iv.Start, iv.End
			return nil
		}
		base = selectReservationBase(file.Reservations, start, size)
		if base < 0 {
			return ErrPortReservationsExhausted
		}
		end = base + size - 1
		file.Reservations[workspaceID] = PortReservationInterval{Start: base, End: end}
		return fsatomic.WriteJSON(s.path, file)
	})
	if err != nil {
		return 0, 0, err
	}
	return base, end, nil
}

// Lookup reads the workspace's persisted interval without allocating. A
// missing registry reports found=false rather than initializing — reads never
// create state. Corrupt data still fails closed.
func (s *PortReservationStore) Lookup(workspaceID string) (base, end int, found bool, err error) {
	if !validReservationID(workspaceID) {
		return 0, 0, false, fmt.Errorf("%w: workspace ID %q is not a valid reservation key", ErrPortReservationsInvalid, workspaceID)
	}
	err = s.withLock(true, func() error {
		file, _, err := s.readLocked()
		if err != nil {
			return err
		}
		if file == nil {
			return nil
		}
		if iv, ok := file.Reservations[workspaceID]; ok {
			base, end, found = iv.Start, iv.End, true
		}
		return nil
	})
	if err != nil {
		return 0, 0, false, err
	}
	return base, end, found, nil
}

// withLock serializes fn under the registry flock — exclusive for writes and
// initialization, shared for pure reads. The same lock ordering applies to
// Initialize and every reservation transaction. Spurious-ENOENT retry lives
// inside lockRegistryFile (registry_lock_open.go) so every store — not just
// this one — rides out the macOS openat-under-os.Root race.
func (s *PortReservationStore) withLock(shared bool, fn func() error) error {
	lockFile, err := lockRegistryFile(s.lockPath, shared)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)
	return fn()
}

// loadOrInitializeLocked returns the validated envelope, creating one through
// the injected guard when the file is missing. A failed guard leaves no
// registry; a refused read never produces an empty-map substitute. Callers
// must hold the exclusive lock.
func (s *PortReservationStore) loadOrInitializeLocked() (*portReservationFile, error) {
	file, missing, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	if !missing {
		return file, nil
	}
	if guard := s.currentGuard(); guard != nil {
		if err := guard(); err != nil {
			return nil, err
		}
	}
	file = &portReservationFile{
		Version:      portReservationsFileVersion,
		Reservations: map[string]PortReservationInterval{},
	}
	if err := fsatomic.WriteJSON(s.path, file); err != nil {
		return nil, err
	}
	return file, nil
}

func (s *PortReservationStore) currentGuard() func() error {
	s.guardMu.Lock()
	defer s.guardMu.Unlock()
	return s.guard
}

// readLocked loads and fully validates the envelope. missing=true means the
// file does not exist; the caller decides whether that is a creation point or
// an empty result. Any other failure is a typed error and the bytes stay
// untouched.
func (s *PortReservationStore) readLocked() (file *portReservationFile, missing bool, err error) {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	file, err = parsePortReservations(raw)
	if err != nil {
		return nil, false, err
	}
	return file, false, nil
}

// parsePortReservations decodes and validates the whole envelope: schema
// version, every reservation ID, interval bounds, and pairwise non-overlap.
// Duplicate JSON keys in the reservations object are ambiguous and rejected;
// a token-level decode is what detects them (encoding/json silently keeps the
// last duplicate when unmarshalling into a map).
func parsePortReservations(raw []byte) (*portReservationFile, error) {
	var head struct {
		Version      *int            `json:"version"`
		Reservations json.RawMessage `json:"reservations"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPortReservationsInvalid, err)
	}
	if head.Version == nil {
		return nil, fmt.Errorf("%w: missing schema version", ErrPortReservationsInvalid)
	}
	if *head.Version > portReservationsFileVersion {
		return nil, fmt.Errorf("port-reservations.json schema version %d (newest known: %d): %w",
			*head.Version, portReservationsFileVersion, ErrUnsupportedSchemaVersion)
	}
	if *head.Version != portReservationsFileVersion {
		return nil, fmt.Errorf("%w: schema version %d", ErrPortReservationsInvalid, *head.Version)
	}

	reservations := map[string]PortReservationInterval{}
	if head.Reservations == nil {
		return &portReservationFile{Version: *head.Version, Reservations: reservations}, nil
	}
	parsed, err := parseReservationEntries(head.Reservations)
	if err != nil {
		return nil, err
	}
	if err := validateNoReservationOverlap(parsed); err != nil {
		return nil, err
	}
	return &portReservationFile{Version: *head.Version, Reservations: parsed}, nil
}

// parseReservationEntries walks the reservations object at token level so a
// duplicated ID — silently collapsed by a map unmarshal — is caught as
// ambiguous input instead of picking one interval arbitrarily.
func parseReservationEntries(raw json.RawMessage) (map[string]PortReservationInterval, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: reservations: %w", ErrPortReservationsInvalid, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("%w: reservations must be an object", ErrPortReservationsInvalid)
	}
	out := make(map[string]PortReservationInterval)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: reservations: %w", ErrPortReservationsInvalid, err)
		}
		id, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("%w: reservations key is not a string", ErrPortReservationsInvalid)
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("%w: duplicate reservation key %q", ErrPortReservationsInvalid, id)
		}
		if !validReservationID(id) {
			return nil, fmt.Errorf("%w: invalid reservation key %q", ErrPortReservationsInvalid, id)
		}
		var iv PortReservationInterval
		if err := dec.Decode(&iv); err != nil {
			return nil, fmt.Errorf("%w: reservation %q: %w", ErrPortReservationsInvalid, id, err)
		}
		if !validReservationInterval(iv) {
			return nil, fmt.Errorf("%w: reservation %q has invalid interval [%d,%d]", ErrPortReservationsInvalid, id, iv.Start, iv.End)
		}
		out[id] = iv
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, fmt.Errorf("%w: reservations: %w", ErrPortReservationsInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: trailing data after reservations", ErrPortReservationsInvalid)
		}
		return nil, fmt.Errorf("%w: reservations: %w", ErrPortReservationsInvalid, err)
	}
	return out, nil
}

// validateNoReservationOverlap rejects envelopes whose intervals collide —
// two IDs claiming the same port is exactly the state the registry exists to
// prevent, so accepting it would launder corruption into allocation.
func validateNoReservationOverlap(reservations map[string]PortReservationInterval) error {
	type entry struct {
		id string
		iv PortReservationInterval
	}
	list := make([]entry, 0, len(reservations))
	for id, iv := range reservations {
		list = append(list, entry{id: id, iv: iv})
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			a, b := list[i], list[j]
			if a.iv.Start <= b.iv.End && a.iv.End >= b.iv.Start {
				return fmt.Errorf("%w: reservations %q [%d,%d] and %q [%d,%d] overlap",
					ErrPortReservationsInvalid, a.id, a.iv.Start, a.iv.End, b.id, b.iv.Start, b.iv.End)
			}
		}
	}
	return nil
}

// selectReservationBase picks the first configured-size interval at the
// configured base that overlaps no persisted interval. Candidates step on
// size-aligned boundaries from start so allocation is deterministic across
// instances. -1 means the space is exhausted.
func selectReservationBase(reservations map[string]PortReservationInterval, start, size int) int {
	const maxPort = 65535
	for base := start; base+size-1 <= maxPort; base += size {
		end := base + size - 1
		free := true
		for _, iv := range reservations {
			if base <= iv.End && end >= iv.Start {
				free = false
				break
			}
		}
		if free {
			return base
		}
	}
	return -1
}

// validReservationRange checks the configured base/width can express at least
// one legal interval, before any allocation arithmetic runs.
func validReservationRange(start, size int) bool {
	const maxPort = 65535
	return size > 0 && start >= 1 && start <= maxPort && size <= maxPort-start+1
}

// validReservationInterval checks one persisted interval: positive width,
// ordered bounds, and wholly inside the TCP port space.
func validReservationInterval(iv PortReservationInterval) bool {
	const maxPort = 65535
	return iv.Start >= 1 && iv.End <= maxPort && iv.Start <= iv.End
}

// validReservationID keeps reservation keys inside the shape a metadata store
// key can have: nonempty, bounded, filename-safe (it is also a metadata
// filename), and free of whitespace and control bytes.
func validReservationID(id string) bool {
	if id == "" || len(id) > 128 || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}
