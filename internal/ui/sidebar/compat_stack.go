package sidebar

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// Temporary shims so the pre-refactor internal/app code compiles while the
// stack lands in order; removed by the app commit.
type (
	BranchChangesLoaded        = messages.BranchChangesLoaded
	AheadBehindLoaded          = messages.AheadBehindLoaded
	SidebarSelectionScrollTick = messages.SidebarSelectionScrollTick
)

// OpenFileInEditor stays a distinct struct (not an alias): the old app layer
// field-copies it into messages.OpenFileInVim.
type OpenFileInEditor struct {
	Path      string
	Workspace *data.Workspace
}
