package update

import (
	"os"
	"path/filepath"
	"testing"
)

// lockedDir creates a read-only directory containing a mode-0777 binary, i.e.
// the shape of a sudo-installed /usr/local/bin: the file looks writable, the
// directory it lives in is not.
func lockedDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bin := filepath.Join(dir, "medusa")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0777); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	return bin
}

func TestCanWriteWritableDir(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "medusa")
	if err := os.WriteFile(bin, []byte("x"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if !CanWrite(bin) {
		t.Error("CanWrite should be true for a binary in a writable directory")
	}
}

// InstallBinary stages a sibling file and renames over the target, so it needs
// a writable *directory*. A writable file in a read-only directory cannot be
// upgraded, and CanWrite must say so.
func TestCanWriteWritableFileInReadOnlyDir(t *testing.T) {
	bin := lockedDir(t)
	if CanWrite(bin) {
		t.Error("CanWrite should be false when the binary's directory is read-only")
	}
}

func TestCanWriteMissingDir(t *testing.T) {
	if CanWrite("/this/path/definitely/does/not/exist/binary") {
		t.Error("CanWrite should be false for a non-existent deep path")
	}
}

// CanWrite must not leave its probe file behind.
func TestCanWriteLeavesNoResidue(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "medusa")
	if err := os.WriteFile(bin, []byte("x"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if !CanWrite(bin) {
		t.Fatal("expected writable")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "medusa" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("CanWrite left residue behind: %v", names)
	}
}

func TestSelfUpdateStatusBlocked(t *testing.T) {
	tests := []struct {
		name   string
		status SelfUpdateStatus
		want   bool
	}{
		{"writable install", SelfUpdateStatus{Writable: true}, false},
		{"read-only install dir", SelfUpdateStatus{Writable: false}, true},
		{"go install is not blocked on permissions", SelfUpdateStatus{GoInstall: true, Writable: false}, false},
		{"unresolvable path is not reported as blocked", SelfUpdateStatus{Err: os.ErrNotExist}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Blocked(); got != tt.want {
				t.Errorf("Blocked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckSelfUpdateResolvesRunningBinary(t *testing.T) {
	status := CheckSelfUpdate()
	if status.Err != nil {
		t.Skipf("cannot resolve test binary path: %v", status.Err)
	}
	if status.BinaryPath == "" {
		t.Error("BinaryPath should be populated when Err is nil")
	}
	// The go test binary lives in a writable temp dir, so this must not be
	// reported as blocked — guards against a check that always fails closed.
	if status.Blocked() {
		t.Errorf("test binary at %s reported as blocked", status.BinaryPath)
	}
}
