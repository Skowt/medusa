package update

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Skowt/medusa/internal/logging"
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

	// Check if go install user
	if IsGoInstall() {
		return fmt.Errorf("installed via 'go install'; run: go install github.com/Skowt/medusa/cmd/medusa@latest")
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

	// Check write permission. Don't suggest sudo: users on managed laptops often
	// can't run it at all, and it's what put the binary somewhere unwritable.
	if !CanWrite(currentBinary) {
		return fmt.Errorf("cannot write to %s; reinstall with: %s",
			filepath.Dir(currentBinary), ReinstallCommand)
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
	tmpDir, err := os.MkdirTemp("", "medusa-update-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Download archive
	archivePath := filepath.Join(tmpDir, asset.Name)
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}

	logging.Info("Downloading %s", asset.Name)
	if err := u.github.DownloadAsset(asset.BrowserDownloadURL, archiveFile); err != nil {
		_ = archiveFile.Close()
		return fmt.Errorf("downloading: %w", err)
	}
	_ = archiveFile.Close()

	// Verify checksum
	logging.Info("Verifying checksum")
	if err := VerifyChecksum(archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract binaries. Secondary helper binaries are optional — older
	// releases or custom builds may omit them.
	logging.Info("Extracting binaries")
	extracted, err := ExtractBinaries(archivePath, tmpDir, append([]string{"medusa"}, secondaryBinaries...))
	if err != nil {
		return fmt.Errorf("extracting binaries: %w", err)
	}
	newMedusa, ok := extracted["medusa"]
	if !ok {
		return fmt.Errorf("medusa binary not found in archive")
	}

	// Install the primary binary. Its atomic swap with rollback is handled
	// inside InstallBinary.
	logging.Info("Installing medusa to %s", currentBinary)
	if err := InstallBinary(newMedusa, currentBinary); err != nil {
		return fmt.Errorf("installing medusa: %w", err)
	}

	installSecondaryBinaries(extracted, currentBinary)

	logging.Info("Upgrade complete: %s", release.TagName)
	return nil
}

// secondaryBinaries are the helper binaries shipped alongside medusa in
// release archives. It is upgraded with the primary binary so the hooks
// injected into Claude profiles always match the running medusa.
var secondaryBinaries = []string{"medusa-hook-emit"}

// installSecondaryBinaries installs each extracted helper next to medusa
// (best-effort). A failure doesn't roll back the primary update; we log and
// continue so the user isn't stuck between versions. Helpers absent from the
// archive are skipped.
func installSecondaryBinaries(extracted map[string]string, currentBinary string) {
	for _, name := range secondaryBinaries {
		staged, ok := extracted[name]
		if !ok {
			logging.Info("%s not in archive; skipping", name)
			continue
		}
		target := filepath.Join(filepath.Dir(currentBinary), name)
		logging.Info("Installing %s to %s", name, target)
		if err := InstallBinary(staged, target); err != nil {
			logging.Warn("Failed to install %s: %v (medusa itself was updated successfully)", name, err)
		}
	}
}

// Version returns the current version.
func (u *Updater) Version() string {
	return u.version
}
