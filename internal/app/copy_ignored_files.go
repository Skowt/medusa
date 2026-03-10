package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
)

// copyIgnoredFiles copies all gitignored files from the source repo into the
// destination worktree, preserving directory structure. It uses
// "git ls-files --others --ignored --exclude-standard" to discover which files
// are present but ignored by .gitignore rules (e.g. .env, secrets, credentials).
func copyIgnoredFiles(repoPath, dstPath string) {
	output, err := git.RunGit(repoPath, "ls-files", "--others", "--ignored", "--exclude-standard")
	if err != nil {
		logging.Warn("Failed to list ignored files in %s: %v", repoPath, err)
		return
	}

	for _, relPath := range strings.Split(strings.TrimSpace(output), "\n") {
		if relPath == "" {
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
	}
}

func copyFile(src, dst string) {
	content, err := os.ReadFile(src)
	if err != nil {
		logging.Warn("Failed to read %s: %v", src, err)
		return
	}
	info, err := os.Stat(src)
	if err != nil {
		logging.Warn("Failed to stat %s: %v", src, err)
		return
	}
	if err := os.WriteFile(dst, content, info.Mode()); err != nil {
		logging.Warn("Failed to write %s: %v", dst, err)
	}
}
