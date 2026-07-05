package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/logging"
)

// upgradeDeps holds the package-level functions Upgrade depends on, wired
// through function fields so the orchestration can be unit-tested without
// network access or replacing the running binary. NewUpdater defaults every
// field to the real implementation; tests override individual fields.
type upgradeDeps struct {
	isHomebrewBuild func() bool
	isGoInstall     func() bool
	findAsset       func(*Release) *Asset
	currentBinary   func() (string, error)
	canWrite        func(string) bool
	fetchChecksums  func(*Release) (map[string]string, error)
	download        func(url string, w io.Writer) error
	verify          func(path, sum string) error
	extract         func(archive, dir string) (string, error)
	install         func(src, dst string) error
}

// CheckResult contains the result of an update check.
type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseNotes    string
	Release         *Release
}

// Updater orchestrates the check and upgrade workflow.
type Updater struct {
	version string
	commit  string
	date    string
	github  *GitHubClient
	deps    upgradeDeps
}

// NewUpdater creates a new Updater.
func NewUpdater(version, commit, date string) *Updater {
	github := NewGitHubClient()
	return &Updater{
		version: version,
		commit:  commit,
		date:    date,
		github:  github,
		deps: upgradeDeps{
			isHomebrewBuild: IsHomebrewBuild,
			isGoInstall:     IsGoInstall,
			findAsset:       FindPlatformAsset,
			currentBinary:   GetCurrentBinaryPath,
			canWrite:        CanWrite,
			fetchChecksums:  github.FetchChecksums,
			download:        github.DownloadAsset,
			verify:          VerifyChecksum,
			extract:         ExtractBinary,
			install:         InstallBinary,
		},
	}
}

// Check checks for available updates.
func (u *Updater) Check() (*CheckResult, error) {
	if IsHomebrewBuild() {
		logging.Debug("Skipping update check for Homebrew build")
		return &CheckResult{
			CurrentVersion:  u.version,
			UpdateAvailable: false,
		}, nil
	}
	// Skip check for dev builds
	if IsDevBuild(u.version) {
		logging.Debug("Skipping update check for dev build")
		return &CheckResult{
			CurrentVersion:  u.version,
			UpdateAvailable: false,
		}, nil
	}

	release, err := u.github.FetchLatestRelease()
	if err != nil {
		return nil, fmt.Errorf("fetching latest release: %w", err)
	}

	currentVer, err := ParseVersion(u.version)
	if err != nil {
		return nil, fmt.Errorf("parsing current version: %w", err)
	}

	latestVer, err := ParseVersion(release.TagName)
	if err != nil {
		return nil, fmt.Errorf("parsing latest version: %w", err)
	}

	updateAvailable := currentVer.LessThan(latestVer)
	logging.Debug("Update check: current=%s latest=%s available=%v",
		currentVer.String(), latestVer.String(), updateAvailable)

	return &CheckResult{
		CurrentVersion:  currentVer.String(),
		LatestVersion:   latestVer.String(),
		UpdateAvailable: updateAvailable,
		ReleaseNotes:    release.Body,
		Release:         release,
	}, nil
}

// Upgrade downloads and installs the latest version.
func (u *Updater) Upgrade(release *Release) error {
	if release == nil {
		return errors.New("no release to upgrade to")
	}

	if u.deps.isHomebrewBuild() {
		return errors.New("installed via Homebrew; run: brew upgrade amux")
	}

	// Check if go install user
	if u.deps.isGoInstall() {
		return errors.New("installed via 'go install'; run: go install github.com/andyrewlee/amux/cmd/amux@latest")
	}

	// Find the platform asset
	asset := u.deps.findAsset(release)
	if asset == nil {
		return errors.New("no binary available for this platform")
	}

	// Get current binary path
	currentBinary, err := u.deps.currentBinary()
	if err != nil {
		return fmt.Errorf("getting current binary path: %w", err)
	}

	// Check write permission
	if !u.deps.canWrite(currentBinary) {
		return fmt.Errorf("no write permission to %s; try running with sudo", currentBinary)
	}

	// Fetch checksums
	checksums, err := u.deps.fetchChecksums(release)
	if err != nil {
		return fmt.Errorf("fetching checksums: %w", err)
	}

	expectedChecksum, ok := checksums[asset.Name]
	if !ok {
		return fmt.Errorf("checksum not found for %s", asset.Name)
	}

	// Create temp directory for download
	tmpDir, err := os.MkdirTemp("", "amux-update-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download archive
	archivePath := filepath.Join(tmpDir, asset.Name)
	archiveFile, err := openFileInParentRoot(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}

	logging.Info("Downloading %s", asset.Name)
	if err := u.deps.download(asset.BrowserDownloadURL, archiveFile); err != nil {
		if closeErr := archiveFile.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("downloading: %w", err),
				fmt.Errorf("closing archive file after failed download: %w", closeErr),
			)
		}
		return fmt.Errorf("downloading: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return fmt.Errorf("closing archive file: %w", err)
	}

	// Verify checksum
	logging.Info("Verifying checksum")
	if err := u.deps.verify(archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract binary
	logging.Info("Extracting binary")
	newBinary, err := u.deps.extract(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("extracting binary: %w", err)
	}

	// Install binary
	logging.Info("Installing to %s", currentBinary)
	if err := u.deps.install(newBinary, currentBinary); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}

	logging.Info("Upgrade complete: %s", release.TagName)
	return nil
}
