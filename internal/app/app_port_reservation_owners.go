package app

import (
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
)

// collectPortReservationOwnerIDs returns the owner set the manual
// port-reservation release protects: everything the shared live collector
// knows (live workspaces, in-flight creates, mid-mutation identities,
// legacy aliases) plus every retained shelf. A shelved workspace
// deliberately keeps its durable port range — the shelf contract retains
// the record for later restore, so the range must not be reassigned while
// the record exists. Shelves live outside Project.Workspaces on purpose:
// the shared live collector must stay shelf-free because tmux orphan GC
// and UI traversal have a different contract — do not merge this into
// collectKnownWorkspaceIDs.
func (a *App) collectPortReservationOwnerIDs() map[string]bool {
	ids := a.collectKnownWorkspaceIDs()
	for i := range a.projects {
		for j := range a.projects[i].ShelvedWorkspaces {
			for _, id := range workspacesvc.WorkspaceMetadataIDs(&a.projects[i].ShelvedWorkspaces[j]) {
				ids[string(id)] = true
			}
		}
	}
	return ids
}
