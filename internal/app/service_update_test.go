package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/update"
)

// These tests exercise updateService in service_update.go. The type is a thin
// adapter around update.NewUpdater(...). newUpdateService is a pure constructor
// and is asserted directly. Check and Upgrade are driven only down their
// deterministic, offline branches:
//
//   - Check is asserted for dev-build versions ("", "dev", "none", "unknown"),
//     where update.Updater.Check short-circuits to a no-update result before any
//     GitHub network call.
//   - Upgrade is asserted for the nil-release guard, which returns an error
//     before any download/extract/install work.
//
// IsHomebrewBuild wraps update.IsHomebrewBuild, whose backing flag is an
// unexported package-level variable in internal/update; it cannot be toggled
// from this package, so the test asserts the default (non-Homebrew) build and
// that the wrapper agrees with the underlying function.
//
// Skipped: the network branch of Check (non-dev versions hit the GitHub API) and
// the upgrade branches of Upgrade past the nil guard (they fetch checksums,
// download, extract, and write the binary, and depend on the live process's own
// install path). Neither has an offline seam in this package, and the SPEC
// forbids changing production code, so they are left to internal/update's own
// network-aware tests rather than re-exercised here as coverage theater.

// newUpdateService must store exactly the fields it is handed and never return
// nil, including for empty inputs.
func TestNewUpdateService(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		commit    string
		buildDate string
	}{
		{
			name:      "fully populated",
			version:   "v1.2.3",
			commit:    "abc123",
			buildDate: "2026-06-13",
		},
		{
			name:      "all empty",
			version:   "",
			commit:    "",
			buildDate: "",
		},
		{
			name:      "dev sentinel version",
			version:   "dev",
			commit:    "none",
			buildDate: "unknown",
		},
		{
			name:      "version only",
			version:   "v0.0.1",
			commit:    "",
			buildDate: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newUpdateService(tt.version, tt.commit, tt.buildDate)
			if svc == nil {
				t.Fatal("newUpdateService returned nil")
			}
			if svc.version != tt.version {
				t.Errorf("version = %q, want %q", svc.version, tt.version)
			}
			if svc.commit != tt.commit {
				t.Errorf("commit = %q, want %q", svc.commit, tt.commit)
			}
			if svc.buildDate != tt.buildDate {
				t.Errorf("buildDate = %q, want %q", svc.buildDate, tt.buildDate)
			}
		})
	}
}

// updateService must satisfy the UpdateService interface the App depends on, so
// the concrete type stays assignable where the interface is required. This is a
// compile-time guard: if updateService stops implementing UpdateService, this
// declaration fails to build.
var _ UpdateService = (*updateService)(nil)

// Check must short-circuit to a deterministic, offline no-update result for
// dev-build versions. update.IsDevBuild treats "", "dev", "none" and "unknown"
// as dev builds; for each, Check echoes the current version, reports no update
// available, and carries no release. These cases never touch the GitHub API.
func TestUpdateService_Check_DevBuild(t *testing.T) {
	devVersions := []struct {
		name    string
		version string
	}{
		{name: "empty version", version: ""},
		{name: "whitespace version", version: "  "},
		{name: "literal dev", version: "dev"},
		{name: "literal none", version: "none"},
		{name: "literal unknown", version: "unknown"},
	}

	for _, tt := range devVersions {
		t.Run(tt.name, func(t *testing.T) {
			svc := newUpdateService(tt.version, "deadbeef", "2026-06-13")
			res, err := svc.Check()
			if err != nil {
				t.Fatalf("Check(%q) returned error: %v", tt.version, err)
			}
			if res == nil {
				t.Fatalf("Check(%q) returned nil result", tt.version)
			}
			if res.UpdateAvailable {
				t.Errorf("Check(%q).UpdateAvailable = true, want false for a dev build", tt.version)
			}
			if res.CurrentVersion != tt.version {
				t.Errorf("Check(%q).CurrentVersion = %q, want %q", tt.version, res.CurrentVersion, tt.version)
			}
			if res.Release != nil {
				t.Errorf("Check(%q).Release = %+v, want nil for a dev build", tt.version, res.Release)
			}
			if res.LatestVersion != "" {
				t.Errorf("Check(%q).LatestVersion = %q, want empty for a dev build", tt.version, res.LatestVersion)
			}
		})
	}
}

// Upgrade must reject a nil release before performing any download or install
// work, and the error must explain that there is nothing to upgrade to.
func TestUpdateService_Upgrade_NilRelease(t *testing.T) {
	svc := newUpdateService("v1.0.0", "abc", "2026-06-13")
	err := svc.Upgrade(nil)
	if err == nil {
		t.Fatal("Upgrade(nil) returned nil error, want a guard error")
	}
	if got := err.Error(); got != "no release to upgrade to" {
		t.Fatalf("Upgrade(nil) error = %q, want %q", got, "no release to upgrade to")
	}
}

// The nil-release guard must hold regardless of how the service was constructed,
// including a zero-value (all-empty) service.
func TestUpdateService_Upgrade_NilRelease_EmptyService(t *testing.T) {
	svc := newUpdateService("", "", "")
	if err := svc.Upgrade(nil); err == nil {
		t.Fatal("Upgrade(nil) on empty service returned nil error, want a guard error")
	}
}

// IsHomebrewBuild must mirror the underlying update.IsHomebrewBuild. The backing
// flag is unexported in internal/update and defaults to a non-Homebrew build, so
// the wrapper must report false and agree with the package-level function.
func TestUpdateService_IsHomebrewBuild(t *testing.T) {
	svc := newUpdateService("v1.0.0", "abc", "2026-06-13")
	got := svc.IsHomebrewBuild()
	if got != update.IsHomebrewBuild() {
		t.Errorf("IsHomebrewBuild() = %v, want it to match update.IsHomebrewBuild() = %v", got, update.IsHomebrewBuild())
	}
	if got {
		t.Error("IsHomebrewBuild() = true, want false for the default (non-Homebrew) build")
	}
}

// IsHomebrewBuild must not depend on the version/commit/buildDate the service was
// constructed with: it is a property of how the binary was built, not of the
// service instance.
func TestUpdateService_IsHomebrewBuild_IndependentOfFields(t *testing.T) {
	a := newUpdateService("v1.0.0", "abc", "2026-06-13")
	b := newUpdateService("", "", "")
	if a.IsHomebrewBuild() != b.IsHomebrewBuild() {
		t.Errorf("IsHomebrewBuild() differed across instances: %v vs %v", a.IsHomebrewBuild(), b.IsHomebrewBuild())
	}
}
