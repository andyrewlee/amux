package data

import (
	"testing"

	"github.com/andyrewlee/amux/internal/validation"
)

// renameWorkspaceLabelDesign is the proposed Tier-1 (label-only) rename gate,
// declared here as a compile-checked design skeleton — it is NOT wired into the
// store or the UI. It isolates the name-validation half of the future
// (*WorkspaceStore).Rename method so the design's reuse of
// validation.ValidateWorkspaceName can be exercised before the feature is built.
//
// See the "Workspace Rename Design (spike)" notes, kept with the maintainer's
// planning notes, for the full two-tier design and the store API this stub
// stands in for.
func renameWorkspaceLabelDesign(name string) error {
	return validation.ValidateWorkspaceName(name)
}

// TestRenameWorkspaceLabelDesign_RejectsInvalidNames exercises the non-wired
// Tier-1 validation gate: a valid label is accepted and every rule enforced by
// validation.ValidateWorkspaceName rejects the corresponding label.
func TestRenameWorkspaceLabelDesign_RejectsInvalidNames(t *testing.T) {
	if err := renameWorkspaceLabelDesign("feature-2"); err != nil {
		t.Fatalf("valid name should be accepted, got %v", err)
	}

	invalid := map[string]string{
		"empty":            "",
		"consecutive dots": "a..b",
		"reserved HEAD":    "HEAD",
		"dot-lock suffix":  "work.lock",
		"leading dash":     "-nope",
	}
	for label, name := range invalid {
		if err := renameWorkspaceLabelDesign(name); err == nil {
			t.Errorf("%s: name %q should be rejected", label, name)
		}
	}
}

// TestRenameWorkspaceLabelDesign_StoreIntegration is a placeholder for the real
// Tier-1 test once (*WorkspaceStore).Rename is implemented: rename a saved
// workspace, reload, and assert Name updated while ID() is unchanged and the
// metadata file did not move. Skipped because the wired method does not exist
// yet — this file is a design skeleton only.
func TestRenameWorkspaceLabelDesign_StoreIntegration(t *testing.T) {
	t.Skip("design skeleton: (*WorkspaceStore).Rename is not implemented yet")
}
