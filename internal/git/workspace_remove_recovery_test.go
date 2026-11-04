package git

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkspaceRemovalMachineFormallyChecked verifies the removal-recovery
// protocol by exhaustive model checking: destructive positions have exactly
// one legal entry, are unreachable from a healthy registration without a
// force failure, are unreachable from a proven-gone dir, and both decision
// functions are exact over their whole input spaces.
func TestWorkspaceRemovalMachineFormallyChecked(t *testing.T) {
	if err := WorkspaceRemovalCheck(); err != nil {
		t.Fatalf("workspace removal machine failed model checking: %v", err)
	}
}

// TestRetryMetadataGuardFormallyChecked verifies the retry-metadata guard:
// a marker owns its path only with a present dir plus its proof (absent
// .git for legacy markers, equal fingerprint otherwise), so a recreated
// worktree is never pruned by an old marker.
func TestRetryMetadataGuardFormallyChecked(t *testing.T) {
	if err := retryGuardCheck(); err != nil {
		t.Fatalf("retry-metadata guard failed model checking: %v", err)
	}
}

func TestClassifyWorkspaceRemoval(t *testing.T) {
	cases := []struct {
		name   string
		marker bool
		probes removalProbes
		want   removalClass
	}{
		{
			// The durable marker wins over every other sentinel.
			name:   "pending marker resumes even when registered",
			marker: true,
			probes: removalProbes{ownership: ownershipRegistered},
			want:   removalClassResumeMarker,
		},
		{
			name:   "registered worktree force-removes",
			probes: removalProbes{ownership: ownershipRegistered},
			want:   removalClassForceRemove,
		},
		{
			name:   "git-proven unregistered cleans up",
			probes: removalProbes{ownership: ownershipUnregistered},
			want:   removalClassCleanUnregistered,
		},
		{
			name:   "proven-gone dir succeeds",
			probes: removalProbes{ownership: ownershipUnknown, dir: sentinelAbsent},
			want:   removalClassSucceedAbsent,
		},
		{
			// Ownership unprovable with the dir intact: fail closed.
			name:   "dir and .git present under broken git fails",
			probes: removalProbes{ownership: ownershipUnknown, dir: sentinelPresent, gitFile: sentinelPresent},
			want:   removalClassFail,
		},
		{
			name: "retry marker routes to unregistered cleanup",
			probes: removalProbes{
				ownership: ownershipUnknown, dir: sentinelPresent,
				gitFile: sentinelAbsent, retryMarker: true, legacyManaged: true,
			},
			want: removalClassCleanUnregistered,
		},
		{
			// The lone destructive entry combination.
			name: "legacy managed leftover resumes destructively",
			probes: removalProbes{
				ownership: ownershipUnknown, dir: sentinelPresent,
				gitFile: sentinelAbsent, legacyManaged: true,
			},
			want: removalClassLegacyRecovery,
		},
		{
			name: "foreign leftover only validates",
			probes: removalProbes{
				ownership: ownershipUnknown, dir: sentinelPresent,
				gitFile: sentinelAbsent,
			},
			want: removalClassCleanUnregistered,
		},
	}
	for _, tc := range cases {
		if got := classifyWorkspaceRemoval(tc.marker, tc.probes); got != tc.want {
			t.Errorf("%s: classifyWorkspaceRemoval = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestClassifyForceFailure(t *testing.T) {
	cases := []struct {
		name     string
		gitCtx   bool
		gitFile  pathSentinel
		safe     bool
		want     forceFailureClass
		destruct bool
	}{
		{
			// A git context failure authorizes recovery even with .git
			// present: the remove never reached the worktree.
			name:   "git context failure recovers",
			gitCtx: true, gitFile: sentinelPresent,
			want:     forceClassContextRecovery,
			destruct: true,
		},
		{
			name:    "unregistered remnant on safe path recovers",
			gitFile: sentinelAbsent, safe: true,
			want:     forceClassRemnantRecovery,
			destruct: true,
		},
		{
			name:    "unregistered remnant on unsafe path is refused by name",
			gitFile: sentinelAbsent, safe: false,
			want:     forceClassRefuseUnsafe,
			destruct: false,
		},
		{
			name:    ".git still present fails closed",
			gitFile: sentinelPresent, safe: true,
			want:     forceClassFail,
			destruct: false,
		},
		{
			name:    ".git probe error fails closed",
			gitFile: sentinelError, safe: true,
			want:     forceClassFail,
			destruct: false,
		},
	}
	for _, tc := range cases {
		got := classifyForceFailure(tc.gitCtx, tc.gitFile, tc.safe)
		if got != tc.want {
			t.Errorf("%s: classifyForceFailure = %v, want %v", tc.name, got, tc.want)
		}
		if got.destructive() != tc.destruct {
			t.Errorf("%s: destructive = %v, want %v", tc.name, got.destructive(), tc.destruct)
		}
	}
}

func TestProbeSentinel(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if s, err := probeSentinel(existing); err != nil || s != sentinelPresent {
		t.Errorf("probeSentinel(existing) = (%v, %v), want (present, nil)", s, err)
	}
	if s, err := probeSentinel(filepath.Join(dir, "gone")); err != nil || s != sentinelAbsent {
		t.Errorf("probeSentinel(gone) = (%v, %v), want (absent, nil)", s, err)
	}
	// A path UNDER a regular file fails with ENOTDIR — an error probe, not
	// absence. This is the fail-closed property that separates probeSentinel
	// from a bare os.IsNotExist check, and ENOTDIR needs no permission bits,
	// so the test holds even when run as root.
	if s, err := probeSentinel(existing + "/child"); err == nil || s != sentinelError {
		t.Errorf("probeSentinel(file/child) = (%v, %v), want (error, non-nil)", s, err)
	}
}
