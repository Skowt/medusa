package update

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdaterCheckDevBuild(t *testing.T) {
	// Dev builds should skip update checks
	updater := NewUpdater("dev", "none", "unknown")
	result, err := updater.Check()
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.UpdateAvailable {
		t.Errorf("Dev build should not have updates available")
	}
}

func TestGetPlatformAssetName(t *testing.T) {
	// This tests the naming convention matches GoReleaser
	name := GetPlatformAssetName("v1.2.3")

	// Should not have "v" prefix in version part
	if name == "" {
		t.Error("GetPlatformAssetName returned empty string")
	}

	// Should end with .tar.gz
	if len(name) < 7 || name[len(name)-7:] != ".tar.gz" {
		t.Errorf("Expected .tar.gz extension, got %s", name)
	}

	// Should start with medusa_1.2.3_ (no v prefix)
	if len(name) < 12 || name[:12] != "medusa_1.2.3" {
		t.Errorf("Expected medusa_1.2.3 prefix, got %s", name)
	}
}

func TestFindPlatformAsset(t *testing.T) {
	release := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "medusa_1.0.0_darwin_amd64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_amd64.tar.gz"},
			{Name: "medusa_1.0.0_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_arm64.tar.gz"},
			{Name: "medusa_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux_amd64.tar.gz"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		},
	}

	asset := FindPlatformAsset(release)
	// We can't know which platform this runs on, but it should find something or nil
	// At minimum, verify it doesn't panic
	_ = asset
}

func TestParseChecksums(t *testing.T) {
	content := `abc123def456  medusa_1.0.0_darwin_amd64.tar.gz
789xyz000111  medusa_1.0.0_linux_amd64.tar.gz
checksum1234  checksums.txt`

	checksums := parseChecksums(content)

	if len(checksums) != 3 {
		t.Errorf("Expected 3 checksums, got %d", len(checksums))
	}

	if checksums["medusa_1.0.0_darwin_amd64.tar.gz"] != "abc123def456" {
		t.Errorf("Wrong checksum for darwin_amd64")
	}

	if checksums["medusa_1.0.0_linux_amd64.tar.gz"] != "789xyz000111" {
		t.Errorf("Wrong checksum for linux_amd64")
	}
}

func TestIsGoInstall(t *testing.T) {
	// Just verify it doesn't panic
	_ = IsGoInstall()
}

func TestCanWrite(t *testing.T) {
	// Test with a path we definitely can't write to
	canWrite := CanWrite("/this/path/definitely/does/not/exist/binary")
	if canWrite {
		t.Error("Should not be able to write to non-existent deep path")
	}
}

// writeTarGz creates a tar.gz at archivePath containing the given name→content entries.
func writeTarGz(t *testing.T, archivePath string, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	for name, content := range entries {
		hdr := &tar.Header{Name: name, Mode: 0755, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}
	tw.Close()
	gzw.Close()
	f.Close()
}

func TestExtractBinaries_BothPresent(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")
	medusaContent := []byte("#!/bin/sh\necho medusa\n")
	approveContent := []byte("#!/bin/sh\necho approve\n")
	writeTarGz(t, archivePath, map[string][]byte{
		"medusa":                  medusaContent,
		"medusa-approve-compound": approveContent,
	})

	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ExtractBinaries(archivePath, destDir, []string{"medusa", "medusa-approve-compound"})
	if err != nil {
		t.Fatalf("ExtractBinaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 binaries, got %d: %v", len(got), got)
	}
	for name, content := range map[string][]byte{"medusa": medusaContent, "medusa-approve-compound": approveContent} {
		path, ok := got[name]
		if !ok {
			t.Fatalf("missing %s in result", name)
		}
		if path != filepath.Join(destDir, name) {
			t.Errorf("%s: expected path %s, got %s", name, filepath.Join(destDir, name), path)
		}
		read, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(read) != string(content) {
			t.Errorf("%s content mismatch", name)
		}
	}
}

func TestExtractBinaries_OptionalMissing(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")
	writeTarGz(t, archivePath, map[string][]byte{
		"medusa": []byte("medusa"),
	})

	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ExtractBinaries(archivePath, destDir, []string{"medusa", "medusa-approve-compound"})
	if err != nil {
		t.Fatalf("ExtractBinaries: %v", err)
	}
	if _, ok := got["medusa"]; !ok {
		t.Errorf("expected medusa to be extracted")
	}
	if _, ok := got["medusa-approve-compound"]; ok {
		t.Errorf("medusa-approve-compound should not be in result when absent from archive")
	}
}

func TestExtractBinaries_NoneMatch(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")
	writeTarGz(t, archivePath, map[string][]byte{"other-file": []byte("hello")})

	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ExtractBinaries(archivePath, destDir, []string{"medusa"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result when archive has no requested binaries, got %v", got)
	}
}

func TestInstallBinary(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcPath := filepath.Join(tmpDir, "new-medusa")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	// Create destination binary
	destPath := filepath.Join(tmpDir, "medusa")
	if err := os.WriteFile(destPath, []byte("old binary"), 0755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	// Install
	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	// Verify new content
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read dest: %v", err)
	}
	if string(content) != "new binary" {
		t.Errorf("Expected 'new binary', got %s", string(content))
	}

	// Verify backup was cleaned up
	if _, err := os.Stat(destPath + ".bak"); !os.IsNotExist(err) {
		t.Error("Backup file should have been removed")
	}

	// Verify staged file was cleaned up
	if _, err := os.Stat(filepath.Join(tmpDir, ".medusa-upgrade-new")); !os.IsNotExist(err) {
		t.Error("Staged file should have been removed")
	}
}

func TestInstallBinaryCrossDir(t *testing.T) {
	// Test that install works when source is in a different directory
	// This simulates the cross-filesystem scenario
	srcDir := t.TempDir()
	destDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "new-medusa")
	if err := os.WriteFile(srcPath, []byte("new binary content"), 0755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	destPath := filepath.Join(destDir, "medusa")
	if err := os.WriteFile(destPath, []byte("old binary content"), 0755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	// Install from different directory
	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	// Verify new content
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read dest: %v", err)
	}
	if string(content) != "new binary content" {
		t.Errorf("Expected 'new binary content', got %s", string(content))
	}
}
