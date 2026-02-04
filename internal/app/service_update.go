package app

import "github.com/andyrewlee/amux/internal/update"

type updateService struct {
	version   string
	commit    string
	buildDate string
}

func newUpdateService(version, commit, buildDate string) *updateService {
	return &updateService{
		version:   version,
		commit:    commit,
		buildDate: buildDate,
	}
}

func (s *updateService) Check() (*update.CheckResult, error) {
	updater := update.NewUpdater(s.version, s.commit, s.buildDate)
	return updater.Check()
}

func (s *updateService) Upgrade(release *update.Release) error {
	updater := update.NewUpdater(s.version, s.commit, s.buildDate)
	return updater.Upgrade(release)
}

func (s *updateService) IsHomebrewBuild() bool {
	return update.IsHomebrewBuild()
}
