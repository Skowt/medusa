package update

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractBinaries extracts the named binaries from a tar.gz archive into destDir.
// Returns a map of binary name to extracted path for every requested binary that
// was found in the archive. Binaries not present in the archive are simply omitted
// from the result map (not an error). An error is returned only for archive-level
// I/O failures. Callers should check the map for presence of required binaries.
func ExtractBinaries(archivePath string, destDir string, names []string) (map[string]string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	result := make(map[string]string)
	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(header.Name)
		if !wanted[name] {
			continue
		}
		if _, already := result[name]; already {
			continue
		}

		outPath := filepath.Join(destDir, name)
		outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return nil, fmt.Errorf("creating output file %s: %w", name, err)
		}

		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			return nil, fmt.Errorf("extracting %s: %w", name, err)
		}
		outFile.Close()
		result[name] = outPath

		if len(result) == len(wanted) {
			break
		}
	}

	return result, nil
}

// InstallBinary performs an atomic replacement of the current binary.
// It stages the new binary in the same directory as the target to avoid
// cross-filesystem rename issues, then uses rename to atomically swap.
func InstallBinary(newBinaryPath string, currentBinaryPath string) error {
	// Ensure the new binary exists and is executable
	info, err := os.Stat(newBinaryPath)
	if err != nil {
		return fmt.Errorf("checking new binary: %w", err)
	}
	if info.Mode()&0111 == 0 {
		if err := os.Chmod(newBinaryPath, 0755); err != nil {
			return fmt.Errorf("setting executable permission: %w", err)
		}
	}

	// Stage the new binary in the same directory as the target to avoid
	// cross-filesystem rename failures (EXDEV). Use a per-binary staging
	// name so installing multiple binaries in the same directory is safe.
	targetDir := filepath.Dir(currentBinaryPath)
	stagedPath := filepath.Join(targetDir, "."+filepath.Base(currentBinaryPath)+".upgrade-new")

	if err := copyFile(newBinaryPath, stagedPath); err != nil {
		return fmt.Errorf("staging new binary: %w", err)
	}
	defer os.Remove(stagedPath) // Clean up on failure

	// Back up the current binary if it exists. A missing target is valid:
	// it means this is a fresh install of a secondary binary (e.g. a user
	// whose prior install.sh never laid down medusa-approve-compound).
	backupPath := currentBinaryPath + ".bak"
	hasBackup := false
	if err := os.Rename(currentBinaryPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("backing up current binary: %w", err)
		}
	} else {
		hasBackup = true
	}

	// Atomically replace with staged binary (same filesystem, so rename works)
	if err := os.Rename(stagedPath, currentBinaryPath); err != nil {
		if hasBackup {
			_ = os.Rename(backupPath, currentBinaryPath)
		}
		return fmt.Errorf("installing new binary: %w", err)
	}

	if hasBackup {
		_ = os.Remove(backupPath)
	}

	return nil
}

// copyFile copies a file from src to dst, preserving executable permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}

// GetCurrentBinaryPath returns the path to the currently running binary.
func GetCurrentBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("getting executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("resolving symlinks: %w", err)
	}

	return realPath, nil
}

// IsGoInstall returns true if the binary appears to be installed via `go install`.
func IsGoInstall() bool {
	binPath, err := GetCurrentBinaryPath()
	if err != nil {
		return false
	}

	// Typical go install path: $GOPATH/bin or $HOME/go/bin
	home, _ := os.UserHomeDir()
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = filepath.Join(home, "go")
	}

	goBin := filepath.Join(goPath, "bin")
	return strings.HasPrefix(binPath, goBin)
}

// CanWrite checks if we have write permission to the binary path.
func CanWrite(path string) bool {
	// Try to open for writing
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return true
	}

	// Check if parent directory is writable (for rename operation)
	dir := filepath.Dir(path)
	testFile := filepath.Join(dir, ".medusa-write-test")
	f, err = os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}
