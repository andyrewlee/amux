package app

import (
	"context"

	"github.com/andyrewlee/amux/internal/git"
)

type gitStatusService struct {
	manager *git.StatusManager
}

func newGitStatusService(manager *git.StatusManager) *gitStatusService {
	return &gitStatusService{manager: manager}
}

func (s *gitStatusService) Run(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.Run(ctx)
}

func (s *gitStatusService) GetCached(root string) *git.StatusResult {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.GetCached(root)
}

func (s *gitStatusService) UpdateCache(root string, status *git.StatusResult) {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.UpdateCache(root, status)
}

func (s *gitStatusService) Invalidate(root string) {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.Invalidate(root)
}

func (s *gitStatusService) Refresh(root string) (*git.StatusResult, error) {
	return git.GetStatus(root)
}
