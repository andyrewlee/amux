package git

import (
	"context"
	"fmt"
	"os"
)

// MoveWorktree moves a worktree directory owned by repoPath to toPath,
// updating the repository's worktree registration atomically in the same
// operation — the branch, index, and working-tree contents are untouched,
// only the path changes. It is the project-rename primitive: the caller
// decides the new container, git keeps the ownership consistent.
//
// Both paths must be absolute-or-clean relative paths as elsewhere in this
// package. The destination must not exist (git refuses to overwrite, and a
// pre-check keeps the error attribution clear); the source must be a
// registered worktree of repoPath. A locked worktree or one containing
// submodules is refused by git itself and surfaces verbatim.
func MoveWorktree(repoPath, fromPath, toPath string) error {
	if _, err := os.Stat(toPath); err == nil {
		return fmt.Errorf("worktree move destination %s already exists", toPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("worktree move destination %s: %w", toPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ops.worktreeTimeout)
	defer cancel()
	_, err := ops.runGitCtx(ctx, repoPath, "worktree", "move", "--", fromPath, toPath)
	return err
}
