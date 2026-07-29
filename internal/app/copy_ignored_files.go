package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
)

// skippedDirs names gitignored directories that are never worth copying into a new
// worktree: large, machine-specific, and regenerable by whichever tool created them.
// The feature exists to carry across .env files, secrets and credentials — none of
// these hold any.
//
// Virtualenvs are the reason this list exists. A venv's interpreter is a symlink into
// a Python installation, and the binary it points at loads libpython through
// "@rpath/libpython3.x.dylib" with an rpath of "@executable_path/../lib". Relocating
// the venv moves @executable_path, so the interpreter looks for libpython next to its
// new home and aborts. Skipping the directory is both faster and safer than copying it
// (a single uv venv is ~11k files).
var skippedDirs = map[string]bool{
	".venv":         true,
	"venv":          true,
	"node_modules":  true,
	"__pycache__":   true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".ruff_cache":   true,
	".tox":          true,
	".next":         true,
	".turbo":        true,
	".parcel-cache": true,
}

// isSkippedPath reports whether any segment of relPath names a skipped directory.
// git reports paths with forward slashes on every platform, so split on "/" rather
// than filepath.Separator.
func isSkippedPath(relPath string) bool {
	for _, segment := range strings.Split(relPath, "/") {
		if skippedDirs[segment] {
			return true
		}
	}
	return false
}

// copyIgnoredFiles copies all gitignored files from the source repo into the
// destination worktree, preserving directory structure and symlinks. It uses
// "git ls-files --others --ignored --exclude-standard" to discover which files
// are present but ignored by .gitignore rules (e.g. .env, secrets, credentials),
// minus anything under skippedDirs.
func copyIgnoredFiles(repoPath, dstPath string) {
	output, err := git.RunGit(repoPath, "ls-files", "--others", "--ignored", "--exclude-standard")
	if err != nil {
		logging.Warn("Failed to list ignored files in %s: %v", repoPath, err)
		return
	}

	var copied, skipped int
	for _, relPath := range strings.Split(strings.TrimSpace(output), "\n") {
		if relPath == "" {
			continue
		}
		if isSkippedPath(relPath) {
			skipped++
			continue
		}

		srcFile := filepath.Join(repoPath, relPath)
		dstFile := filepath.Join(dstPath, relPath)

		// Ensure parent directory exists
		if dir := filepath.Dir(dstFile); dir != dstPath {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				logging.Warn("Failed to create directory %s: %v", dir, err)
				continue
			}
		}

		copyFile(srcFile, dstFile)
		copied++
	}

	logging.Info("Copied %d ignored files into %s (skipped %d under build/env dirs)", copied, dstPath, skipped)
}

func copyFile(src, dst string) {
	// Recreate symlinks instead of copying what they point at. os.ReadFile and
	// os.Stat both follow links, so dereferencing here would turn a linked
	// interpreter or shared library into a standalone copy that can no longer
	// resolve its @rpath/$ORIGIN-relative dependencies from the new location.
	info, err := os.Lstat(src)
	if err != nil {
		logging.Warn("Failed to stat %s: %v", src, err)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		copySymlink(src, dst)
		return
	}

	content, err := os.ReadFile(src)
	if err != nil {
		logging.Warn("Failed to read %s: %v", src, err)
		return
	}
	if err := os.WriteFile(dst, content, info.Mode().Perm()); err != nil {
		logging.Warn("Failed to write %s: %v", dst, err)
	}
}

// copySymlink reproduces src's link target verbatim at dst, so relative links stay
// relative to their new parent and absolute links keep pointing at the same place.
func copySymlink(src, dst string) {
	target, err := os.Readlink(src)
	if err != nil {
		logging.Warn("Failed to read link %s: %v", src, err)
		return
	}
	// Symlink fails on an existing path; make the copy idempotent.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		logging.Warn("Failed to replace %s: %v", dst, err)
		return
	}
	if err := os.Symlink(target, dst); err != nil {
		logging.Warn("Failed to symlink %s -> %s: %v", dst, target, err)
	}
}
