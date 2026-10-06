package data

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Transient reservation holds
//
// A workspace without a persisted store key cannot mint a durable
// reservation — but a purely local allocation is invisible to every other
// amux instance, so a second instance's durable mint (or another degraded
// instance's local pick) could select the same interval and hand two
// workspaces the same ports. Transient holds solve this by publishing the
// degraded allocation into the shared registry under a
// `transient-<ownerPID>-<rootHash>` key: the registry's mint scan and every
// used-set union then cover it exactly like a durable interval.
//
// Lifecycle contract (deliberately different from durable reservations):
//
//   - A transient hold lives as long as its OWNING PROCESS does. Dead-owner
//     holds are swept inside Reserve's write lock, so a crashed instance's
//     intervals free for reissue instead of burning forever. This is the
//     one sense in which transient ownership can legitimately end while a
//     consumer might exist: a tmux session may outlive its amux process —
//     the residual collision risk is the same one any port registry
//     carries for processes it cannot name, and it is the documented
//     trade-off for self-healing holds.
//   - Deleting the workspace releases the caller's own transient hold
//     (ReleasePort) — the workspace's sessions are torn down by the delete
//     path, so its ports are free.
//   - Transient holds are per-process and never durable: they are filtered
//     out of the reclaim enumeration (ReservedIntervals) so orphan
//     reclamation never lists them.
//   - A workspace that later gains a StoredID mints a normal durable
//     reservation alongside its transient hold — the two keys coexist,
//     intervals disjoint by construction, and the transient hold is swept
//     when the owning process exits.

const transientReservationPrefix = "transient-"

// TransientReservationKey mints the published-hold key for a workspace with
// no durable identity: the owner PID scopes liveness (dead-owner holds are
// swept on the next reservation write); the normalized-root hash scopes the
// workspace deterministically across instances.
func TransientReservationKey(ownerPID int, workspaceRoot string) string {
	sum := sha256.Sum256([]byte(NormalizePath(workspaceRoot)))
	return fmt.Sprintf("%s%d-%s", transientReservationPrefix, ownerPID, hex.EncodeToString(sum[:8]))
}

// IsTransientReservationID reports whether id is a transient published hold.
// Callers enumerating the registry for reclamation must exclude these —
// they are process-scoped and release only through owner death, workspace
// deletion, or the dead-owner sweep.
func IsTransientReservationID(id string) bool {
	return strings.HasPrefix(id, transientReservationPrefix)
}

// transientOwnerPID parses the owner PID out of a transient hold key,
// reporting ok=false for durable IDs and malformed keys.
func transientOwnerPID(id string) (int, bool) {
	if !IsTransientReservationID(id) {
		return 0, false
	}
	rest := id[len(transientReservationPrefix):]
	dash := strings.IndexByte(rest, '-')
	if dash <= 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(rest[:dash])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// sweepDeadTransientReservationsLocked drops every transient hold whose
// owning process is gone. Call inside a write-locked registry transaction —
// the mutation is committed by the enclosing fsatomic write, so a read
// never observes a partially swept file. An unparseable or still-living
// owner keeps its hold (the conservative direction: a leaked interval is
// recoverable, a premature release is a collision).
func sweepDeadTransientReservationsLocked(file *portReservationFile) {
	for id := range file.Reservations {
		pid, ok := transientOwnerPID(id)
		if !ok {
			continue
		}
		if !transientOwnerAlive(pid) {
			delete(file.Reservations, id)
		}
	}
}
