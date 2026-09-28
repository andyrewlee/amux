package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/tmux"
)

// These tests pin the first-adoption guard's safety contract: it runs only
// while the registry is missing, it refuses whenever an amux-owned session
// could be holding an untracked range, it never treats discovery failure as
// an empty list, and it never mistakes another state home's sessions (or a
// non-amux session) for a blocker.

// Instance IDs are "<16-hex namespace>.<16-hex suffix>" (see instance_id.go);
// the namespace is the state-home hash the guard compares.
const testGuardNamespace = "0123456789abcdef"

func otherNamespace() string {
	// A well-formed namespace that differs from testGuardNamespace.
	return "fedcba9876543210"
}

func TestCheckPortReservationAdoptionEmptyServer(t *testing.T) {
	err := checkPortReservationAdoption(testGuardNamespace, func() ([]tmux.SessionTagValues, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("empty server must permit adoption: %v", err)
	}
}

func TestCheckPortReservationAdoptionNonAmuxSessionsDoNotBlock(t *testing.T) {
	err := checkPortReservationAdoption(testGuardNamespace, func() ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{
			{Name: "work", Tags: map[string]string{}},
			{Name: "scratch", Tags: map[string]string{"@amux": "0"}},
		}, nil
	})
	if err != nil {
		t.Fatalf("non-amux sessions must not block: %v", err)
	}
}

func TestCheckPortReservationAdoptionForeignNamespaceDoesNotBlock(t *testing.T) {
	foreign := otherNamespace() + ".0011223344556677"
	err := checkPortReservationAdoption(testGuardNamespace, func() ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{
			{Name: "amux-proj-ws", Tags: map[string]string{
				"@amux":          "1",
				"@amux_instance": foreign,
			}},
		}, nil
	})
	if err != nil {
		t.Fatalf("session provably owned by another state home must not block: %v", err)
	}
}

func TestCheckPortReservationAdoptionSameNamespaceBlocks(t *testing.T) {
	sameHome := testGuardNamespace + ".aabbccddee001122"
	err := checkPortReservationAdoption(testGuardNamespace, func() ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{
			{Name: "amux-proj-ws", Tags: map[string]string{
				"@amux":          "1",
				"@amux_instance": sameHome,
			}},
		}, nil
	})
	if !errors.Is(err, errPortReservationAdoptionBlocked) {
		t.Fatalf("same-home session must block with the typed error: %v", err)
	}
	if !strings.Contains(err.Error(), "amux-proj-ws") {
		t.Fatalf("error must name a blocking session: %v", err)
	}
	if !strings.Contains(err.Error(), "per-process port allocation") {
		t.Fatalf("error must carry the fallback explanation: %v", err)
	}
}

func TestCheckPortReservationAdoptionAmbiguousLegacyBlocks(t *testing.T) {
	// Pre-tag amux sessions carry no @amux_instance at all; an empty or
	// malformed tag can never prove foreign ownership, so the session blocks.
	cases := map[string]tmux.SessionTagValues{
		"prefix only":        {Name: "amux-proj-ws", Tags: map[string]string{}},
		"tagged no instance": {Name: "amux-proj-ws", Tags: map[string]string{"@amux": "1"}},
		"empty instance":     {Name: "amux-proj-ws", Tags: map[string]string{"@amux": "1", "@amux_instance": ""}},
		"malformed instance": {Name: "amux-proj-ws", Tags: map[string]string{"@amux": "1", "@amux_instance": "not-an-instance"}},
	}
	for name, session := range cases {
		err := checkPortReservationAdoption(testGuardNamespace, func() ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{session}, nil
		})
		if !errors.Is(err, errPortReservationAdoptionBlocked) {
			t.Fatalf("%s: ambiguous session must block adoption: %v", name, err)
		}
	}
}

func TestCheckPortReservationAdoptionDiscoveryErrorBlocks(t *testing.T) {
	err := checkPortReservationAdoption(testGuardNamespace, func() ([]tmux.SessionTagValues, error) {
		return nil, errors.New("tmux server unreachable")
	})
	if !errors.Is(err, errPortReservationAdoptionBlocked) {
		t.Fatalf("discovery failure must surface as a blocked adoption, not an empty list: %v", err)
	}
	if !strings.Contains(err.Error(), "tmux server unreachable") {
		t.Fatalf("error must preserve the discovery cause: %v", err)
	}
}

func TestPortReservationGuardNamespacesTheInstance(t *testing.T) {
	// The guard derives the state-home namespace once, from the instance ID —
	// a malformed instance ID must still yield a callable guard that blocks
	// on ambiguous sessions rather than panic or misnamespace.
	guard := portReservationGuard(tmux.Options{}, "malformed")
	if guard == nil {
		t.Fatal("guard must be non-nil")
	}
	// Not invoking it: it would shell out to tmux. The namespacing itself is
	// covered through checkPortReservationAdoption above.
}

// initDurablePortReservations must never keep a user out of amux: a refused
// or failed adoption leaves the registry absent (so a later quiet start can
// adopt), warns visibly, and keeps the runner on the pre-durable allocator.
func newPortReservationTestApp() *App {
	return &App{
		externalMsgs:     make(chan tea.Msg, externalMsgBuffer),
		externalCritical: make(chan tea.Msg, externalCriticalBuffer),
	}
}

func TestInitDurablePortReservationsDegradesOnRefusal(t *testing.T) {
	home := t.TempDir()
	a := newPortReservationTestApp()
	runner := process.NewScriptRunner(16200, 10)
	a.initDurablePortReservations(data.NewPortReservationStore(home),
		func() error { return errors.New("child guard refusal") }, runner)
	if _, err := os.Stat(filepath.Join(home, data.PortReservationRegistryFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("refused adoption must leave the registry absent, stat err = %v", err)
	}
	select {
	case msg := <-a.externalCritical:
		mErr, ok := msg.(messages.Error)
		if !ok {
			t.Fatalf("expected a messages.Error warning, got %#v", msg)
		}
		if !strings.Contains(mErr.Err.Error(), "child guard refusal") {
			t.Fatalf("warning must carry the refusal cause, got %v", mErr.Err)
		}
	default:
		t.Fatal("refused adoption must surface a user-visible warning")
	}
}

func TestInitDurablePortReservationsInstallsOnPermit(t *testing.T) {
	home := t.TempDir()
	a := newPortReservationTestApp()
	runner := process.NewScriptRunner(16200, 10)
	a.initDurablePortReservations(data.NewPortReservationStore(home),
		func() error { return nil }, runner)
	if _, err := os.Stat(filepath.Join(home, data.PortReservationRegistryFile)); err != nil {
		t.Fatalf("permitted adoption must create the registry: %v", err)
	}
	select {
	case msg := <-a.externalCritical:
		t.Fatalf("permitted adoption must not warn, got %#v", msg)
	default:
	}
}

func TestIsAmuxOwnedSession(t *testing.T) {
	cases := []struct {
		name    string
		session tmux.SessionTagValues
		want    bool
	}{
		{"tagged current", tmux.SessionTagValues{Name: "anything", Tags: map[string]string{"@amux": "1"}}, true},
		{"tag value non-zero", tmux.SessionTagValues{Name: "anything", Tags: map[string]string{"@amux": "yes"}}, true},
		{"untagged legacy prefix", tmux.SessionTagValues{Name: "amux-proj-ws", Tags: map[string]string{}}, true},
		{"explicit non-amux tag", tmux.SessionTagValues{Name: "anything", Tags: map[string]string{"@amux": "0"}}, false},
		{"plain foreign session", tmux.SessionTagValues{Name: "editor", Tags: map[string]string{}}, false},
		{"amux-ish but not prefixed", tmux.SessionTagValues{Name: "my-amux-test", Tags: map[string]string{}}, false},
	}
	for _, tc := range cases {
		if got := isAmuxOwnedSession(tc.session); got != tc.want {
			t.Fatalf("%s: isAmuxOwnedSession = %v, want %v", tc.name, got, tc.want)
		}
	}
}
