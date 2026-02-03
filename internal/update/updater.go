package update

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/logging"
)

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
}

// NewUpdater creates a new Updater.
func NewUpdater(version, commit, date string) *Updater {
	return &Updater{
		version: version,
		commit:  commit,
		date:    date,
		github:  NewGitHubClient(),
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
		return fmt.Errorf("no release to upgrade to")
	}

	if IsHomebrewBuild() {
		return fmt.Errorf("installed via Homebrew; run: brew upgrade amux")
	}

	// Check if go install user
	if IsGoInstall() {
		return fmt.Errorf("installed via 'go install'; run: go install github.com/andyrewlee/amux/cmd/amux@latest")
	}

	// Find the platform asset
	asset := FindPlatformAsset(release)
	if asset == nil {
		return fmt.Errorf("no binary available for this platform")
	}

	// Get current binary path
	currentBinary, err := GetCurrentBinaryPath()
	if err != nil {
		return fmt.Errorf("getting current binary path: %w", err)
	}

	// Check write permission
	if !CanWrite(currentBinary) {
		return fmt.Errorf("no write permission to %s; try running with sudo", currentBinary)
	}

	// Fetch checksums
	checksums, err := u.github.FetchChecksums(release)
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
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}

	logging.Info("Downloading %s", asset.Name)
	if err := u.github.DownloadAsset(asset.BrowserDownloadURL, archiveFile); err != nil {
		archiveFile.Close()
		return fmt.Errorf("downloading: %w", err)
	}
	archiveFile.Close()

	// Verify checksum
	logging.Info("Verifying checksum")
	if err := VerifyChecksum(archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract binary
	logging.Info("Extracting binary")
	newBinary, err := ExtractBinary(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("extracting binary: %w", err)
	}

	// Install binary
	logging.Info("Installing to %s", currentBinary)
	if err := InstallBinary(newBinary, currentBinary); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}

	logging.Info("Upgrade complete: %s", release.TagName)
	return nil
}

// Version returns the current version.
func (u *Updater) Version() string {
	return u.version
}
