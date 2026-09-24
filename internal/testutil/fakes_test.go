package testutil

import (
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
)

// Compile-time interface satisfaction for the fakes that have a named,
// importable interface. FakeTmuxOps's structural check lives in
// internal/app (app.TmuxOps is not importable from here).
var (
	_ workspacesvc.GitOperations   = (*FakeGitOps)(nil)
	_ workspacesvc.ProjectRegistry = (*FakeProjectRegistry)(nil)
	_ workspacesvc.WorkspaceStore  = (*FakeWorkspaceStore)(nil)
)
