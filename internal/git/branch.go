package git

import (
	"fmt"
	"strings"
)

// BranchMode selects how the base branch is resolved when creating a workspace.
type BranchMode int

const (
	BranchModeRemoteMain BranchMode = iota // Fetch, then use the repo's default branch
	BranchModeCheckedOut                   // Use current local branch, no fetch
	BranchModeCustom                       // Resolve a named branch locally, then on origin
)

// GetBaseBranch returns the repo's default branch name.
//
// origin/HEAD is git's own record of which branch is the default, so it wins
// whenever it is set. The name guesses below are only a fallback for repos with
// no remote: guessing first would return "main" for a master repo that happens
// to have a stale local main lying around, and never even read origin/HEAD.
//
// The guesses check for a *branch* (BranchExists) rather than any ref that
// resolves. "rev-parse --verify main" also matches a *tag* named main, which is
// not a branch and is usually pinned to some long-past commit — branching a new
// worktree off it silently strands the work in history.
func GetBaseBranch(repoPath string) (string, error) {
	if branch := remoteDefaultBranch(repoPath); branch != "" {
		return branch, nil
	}

	for _, branch := range []string{"main", "master", "develop", "dev"} {
		if BranchExists(repoPath, branch) {
			return branch, nil
		}
	}

	// Nothing to go on — assume the modern default.
	return "main", nil
}

// remoteDefaultBranch reads the branch origin/HEAD points at, e.g.
// "refs/remotes/origin/main" -> "main". Returns "" when origin/HEAD is unset,
// which is the case for a repo with no remote. Trims the whole prefix rather
// than taking the last path segment, so a default branch with a slash in its
// name (release/stable) survives intact.
func remoteDefaultBranch(repoPath string) string {
	output, err := RunGit(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	const prefix = "refs/remotes/origin/"
	ref := strings.TrimSpace(output)
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(ref, prefix)
}

// ValidateRef checks that a ref resolves to a valid commit.
func ValidateRef(repoPath, ref string) error {
	_, err := RunGit(repoPath, "rev-parse", "--verify", ref+"^{commit}")
	return err
}

// GetFreshRemoteBase fetches if stale, then returns "origin/<base>" if it
// exists, falling back to the local base branch name.
func GetFreshRemoteBase(repoPath string) (string, error) {
	// Best-effort fetch; ignore errors (e.g. no network).
	_ = FetchIfStale(repoPath)

	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return base, err
	}

	remote := "origin/" + base
	if _, err := RunGit(repoPath, "rev-parse", "--verify", remote); err == nil {
		return remote, nil
	}
	return base, nil
}

// GetCheckedOutBase returns the current branch name for use as a worktree base.
// No fetch is performed.
func GetCheckedOutBase(repoPath string) (string, error) {
	return GetCurrentBranch(repoPath)
}

// ResolveCustomBranch looks up a branch locally first, then on the remote.
// Returns an error if neither is found. Callers decide what a miss means —
// in a multi-repo workspace a branch that exists in only some repos is not
// fatal, and that judgement needs a view of every repo (see fetchCustomBase).
func ResolveCustomBranch(repoPath, branch string) (string, error) {
	if BranchExists(repoPath, branch) {
		return branch, nil
	}
	remote := "origin/" + branch
	if ValidateRef(repoPath, remote) == nil {
		return remote, nil
	}
	return "", fmt.Errorf("branch %q not found locally or on origin", branch)
}

// GetBranchFileDiff returns the full diff for a single file on the branch
func GetBranchFileDiff(repoPath, path string) (*DiffResult, error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return nil, err
	}

	mergeBase, err := RunGit(repoPath, "merge-base", base, "HEAD")
	if err != nil {
		mergeBase = base
	}

	args := []string{"diff", "--no-color", "--no-ext-diff", "-U3", mergeBase + "...HEAD", "--", path}
	output, err := RunGit(repoPath, args...)
	if err != nil {
		return &DiffResult{
			Path:  path,
			Error: err.Error(),
		}, nil
	}

	return parseDiff(path, output), nil
}
