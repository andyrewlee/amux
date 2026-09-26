package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/tmux"
)

// initDurablePortReservations installs the shared store when its one-time
// adoption guard permits, and otherwise degrades this instance to the
// pre-durable per-process allocator with a logged + user-visible warning. It
// never fails: a refused adoption or an unusable registry must not keep an
// operator out of amux, and transient allocation is exactly what older
// versions ran. The store stays uninstalled on failure, so no registry is
// created or mutated while its safety preconditions are unmet.
func (a *App) initDurablePortReservations(store *data.PortReservationStore, guard func() error, scripts *process.ScriptRunner) {
	if err := store.Initialize(guard); err != nil {
		logging.Warn("Durable port reservations deferred: %v", err)
		a.tryEnqueueExternalMsg(messages.Error{
			Err:     err,
			Context: errorContext(errorServiceApp, "durable port reservations deferred"),
			Logged:  true,
		})
		return
	}
	scripts.SetPortReservationStore(store)
}

// Durable port reservations (internal/data.PortReservationStore) require a
// one-time quiescent migration: an in-memory allocator's choices are
// unrecoverable once its app process exits, so a tmux session left behind by
// an old-version amux holds a range the new registry cannot know. Creating
// the registry while such sessions survive would let it hand their ports to
// a different workspace.
//
// The first-initialization guard below runs only while the registry file is
// missing — the data store invokes it before writing the empty envelope — and
// it NEVER kills sessions or invents ranges: it refuses adoption, the app
// falls back to per-process allocation for that run (the behavior older
// versions always had), and the registry stays absent so a later, quieter
// start can adopt cleanly. Startup is never blocked — an operator who keeps
// amux sessions alive must always be able to open amux. After the registry
// exists it is authoritative; no check runs again.
//
// The limitation is honest: the guard can only inspect THIS app's configured
// tmux server. Legacy sessions on other custom servers merely delay adoption
// past the next quiescent start on this server.

// errPortReservationAdoptionBlocked marks adoption refusals: surviving amux
// sessions make first adoption unsafe, so the run degrades to transient
// allocation instead of writing a registry that could hand their ranges away.
var errPortReservationAdoptionBlocked = errors.New("durable port reservations require a one-time quiescent migration")

// portReservationGuard builds the adoption check the data store runs when the
// registry file is absent. instanceID carries the state-home namespace this
// app's sessions are stamped under — sessions tagged to a DIFFERENT namespace
// belong to a different state home sharing this tmux server and do not block.
func portReservationGuard(opts tmux.Options, instanceID string) func() error {
	namespace, _ := instanceStateNamespace(instanceID)
	return func() error {
		return checkPortReservationAdoption(namespace, func() ([]tmux.SessionTagValues, error) {
			return tmux.SessionsWithTags(nil, []string{"@amux", "@amux_instance"}, opts)
		})
	}
}

// checkPortReservationAdoption lists sessions on the configured tmux server
// and refuses when any amux-owned session could be holding a range minted by
// a pre-durable allocator. Conservative on purpose: an amux session whose
// instance tag is missing or unusable cannot be proven to belong to another
// state home, so it blocks; a session with a well-formed tag for a DIFFERENT
// state-home namespace is someone else's amux and does not. Discovery failure
// is an error, never an empty result.
func checkPortReservationAdoption(namespace string, list func() ([]tmux.SessionTagValues, error)) error {
	sessions, err := list()
	if err != nil {
		return fmt.Errorf("%w: could not inspect the configured tmux server: %w",
			errPortReservationAdoptionBlocked, err)
	}
	var blockers []string
	for _, s := range sessions {
		if !isAmuxOwnedSession(s) {
			continue // a non-amux session is never a blocker
		}
		ns, ok := instanceStateNamespace(strings.TrimSpace(s.Tags["@amux_instance"]))
		if ok && ns != namespace {
			continue // provably owned by a different state home
		}
		blockers = append(blockers, s.Name)
	}
	if len(blockers) == 0 {
		return nil
	}
	sort.Strings(blockers)
	shown := blockers
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return fmt.Errorf("%w: found %d existing amux session(s) on the configured tmux server (e.g. %s);\n"+
		"falling back to per-process port allocation — restart amux while no amux sessions remain to enable durable reservations",
		errPortReservationAdoptionBlocked, len(blockers), strings.Join(shown, ", "))
}

// isAmuxOwnedSession recognizes sessions amux created: current versions stamp
// @amux=1 at creation; pre-tag sessions are identified by the amux- name
// prefix every session name shares (tmux.SessionName("amux", ...)).
func isAmuxOwnedSession(s tmux.SessionTagValues) bool {
	if v := strings.TrimSpace(s.Tags["@amux"]); v != "" && v != "0" {
		return true
	}
	return strings.HasPrefix(s.Name, "amux-")
}
