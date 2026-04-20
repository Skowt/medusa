package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CreateWorkspace creates a new workspace backed by a git worktree
func CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	// Create branch from base and checkout into workspace path
	_, err := RunGit(repoPath, "worktree", "add", "--no-track", "-b", branch, workspacePath, base)
	return err
}

// RemoveWorkspace removes a workspace backed by a git worktree
func RemoveWorkspace(repoPath, workspacePath string) error {
	_, err := RunGit(repoPath, "worktree", "remove", workspacePath, "--force")
	if err != nil {
		// git worktree remove --force unregisters the workspace (removes .git file)
		// but fails to delete the directory if it contains untracked files.
		// If the .git file is gone, the workspace was successfully unregistered
		// and we can safely remove the remaining directory ourselves.
		gitFile := filepath.Join(workspacePath, ".git")
		if _, statErr := os.Stat(gitFile); os.IsNotExist(statErr) {
			return os.RemoveAll(workspacePath)
		}
		return err
	}
	// git worktree remove --force may leave the directory behind if it
	// contains untracked files (e.g. .claude/settings.local.json).
	// Clean up any leftover directory.
	if _, statErr := os.Stat(workspacePath); statErr == nil {
		return os.RemoveAll(workspacePath)
	}
	return nil
}

// PruneWorktrees runs "git worktree prune" to clean up stale worktree entries.
func PruneWorktrees(repoPath string) error {
	_, err := RunGit(repoPath, "worktree", "prune")
	return err
}

// DeleteBranch deletes a git branch
func DeleteBranch(repoPath, branch string) error {
	_, err := RunGit(repoPath, "branch", "-D", branch)
	return err
}

// MoveWorkspace moves a git worktree from oldPath to newPath.
// It first tries "git worktree move", then falls back to a manual move
// for worktrees that contain submodules (which git refuses to move).
func MoveWorkspace(repoPath, oldPath, newPath string) error {
	_, err := RunGit(repoPath, "worktree", "move", oldPath, newPath)
	if err == nil {
		return nil
	}

	// Fallback: manually move the directory and repair the worktree links.
	if renameErr := os.Rename(oldPath, newPath); renameErr != nil {
		return fmt.Errorf("move worktree directory: %w", renameErr)
	}

	// "git worktree repair" fixes the bidirectional link between the
	// worktree's .git file and the main repo's .git/worktrees/<name>/gitdir.
	if _, repairErr := RunGit(newPath, "worktree", "repair"); repairErr != nil {
		// Try to undo the move so we don't leave things half-broken.
		_ = os.Rename(newPath, oldPath)
		return fmt.Errorf("repair worktree after move: %w", repairErr)
	}

	// Re-initialize submodules so their path references point to the new
	// worktree location. This is needed because submodule configs under
	// .git/worktrees/<name>/modules/ contain core.worktree paths that
	// still reference the old worktree directory.
	_, _ = RunGit(newPath, "submodule", "update", "--init", "--recursive")

	return nil
}

// RenameBranch renames a git branch from oldBranch to newBranch.
func RenameBranch(repoPath, oldBranch, newBranch string) error {
	_, err := RunGit(repoPath, "branch", "-m", oldBranch, newBranch)
	return err
}

// BranchExists returns true if the given branch exists in the repository.
func BranchExists(repoPath, branch string) bool {
	output, err := RunGit(repoPath, "branch", "--list", branch)
	if err != nil {
		return false
	}
	return strings.TrimSpace(output) != ""
}

// IsWorktree returns true if the given path is a git worktree (not a main clone).
// A worktree has a .git file (not directory) pointing to the main repo's .git/worktrees/.
func IsWorktree(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// ResolveWorktreeRepo resolves a worktree (or plain clone) directory back to
// the path of the main repository that owns it.
// It first tries git directly, then falls back to parsing the .git file
// for cases where the worktree link is broken (e.g. worktree was moved/copied).
func ResolveWorktreeRepo(worktreePath string) (string, error) {
	// Try git first — works for healthy worktrees and normal clones
	commonDir, err := RunGit(worktreePath, "rev-parse", "--git-common-dir")
	if err == nil {
		commonDir = strings.TrimSpace(commonDir)
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(worktreePath, commonDir)
		}
		repoRoot := filepath.Dir(filepath.Clean(commonDir))
		if IsGitRepository(repoRoot) {
			return repoRoot, nil
		}
	}

	// Fallback: parse .git file directly for broken worktree links.
	// A worktree's .git file contains "gitdir: /path/to/repo/.git/worktrees/<name>".
	// We walk up from the gitdir to find the repo root.
	gitPath := filepath.Join(worktreePath, ".git")
	info, statErr := os.Stat(gitPath)
	if statErr != nil {
		return "", fmt.Errorf("resolve worktree repo %s: %w", worktreePath, err)
	}
	if info.IsDir() {
		// .git is a directory — this is a normal clone, not a worktree.
		// git already failed above, so this repo is broken.
		return "", fmt.Errorf("resolve worktree repo %s: %w", worktreePath, err)
	}

	raw, readErr := os.ReadFile(gitPath)
	if readErr != nil {
		return "", fmt.Errorf("read .git file %s: %w", gitPath, readErr)
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("invalid .git file in %s", worktreePath)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))

	// gitDir is like /path/to/repo/.git/worktrees/<name>
	// Walk up to find the .git dir, then its parent is the repo root.
	dir := gitDir
	for dir != "/" && dir != "." {
		base := filepath.Base(dir)
		dir = filepath.Dir(dir)
		if base == ".git" {
			// dir is now the repo root
			if IsGitRepository(dir) {
				return dir, nil
			}
			break
		}
	}

	return "", fmt.Errorf("could not resolve repo for worktree %s", worktreePath)
}

// ResolveWorktreeGitDir resolves a worktree directory to the main repository's
// .git directory path. This is needed for sandbox SBPL profiles that must
// allowlist the .git dir so git operations work inside the sandbox.
// For a normal clone (where .git is a directory), it returns worktreePath/.git.
// For a worktree (where .git is a file), it parses the gitdir pointer and
// walks up to find the main .git directory.
func ResolveWorktreeGitDir(worktreePath string) (string, error) {
	gitPath := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", fmt.Errorf("stat .git in %s: %w", worktreePath, err)
	}

	// .git is a directory — this is a normal clone
	if info.IsDir() {
		return gitPath, nil
	}

	// .git is a file — this is a worktree, parse the gitdir pointer
	raw, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("read .git file %s: %w", gitPath, err)
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("invalid .git file in %s", worktreePath)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}

	// gitDir is like /path/to/repo/.git/worktrees/<name>
	// Walk up to find the .git directory itself
	dir := filepath.Clean(gitDir)
	for dir != "/" && dir != "." {
		if filepath.Base(dir) == ".git" {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}

	return "", fmt.Errorf("could not resolve .git dir for worktree %s", worktreePath)
}
