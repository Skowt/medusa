package gitreview

import (
	"strings"

	"github.com/Skowt/medusa/internal/git"
)

// baseInfo is the commit a review measures from, and how to name it on screen.
type baseInfo struct {
	// rev is what the diff is taken against.
	rev string
	// label is the human name for it: the base branch, or HEAD.
	label string
	// short and subject describe rev itself, so the page can show which commit
	// it actually landed on rather than only the branch it came from.
	short   string
	subject string
}

// resolveBase works out what the review is a diff *from*.
//
// It runs on every refresh rather than once when the page opens, and that is the
// point. A rebase halfway through a session moves the fork point, and a base
// resolved at open time would keep measuring from the old one -- so every commit
// the rebase brought in would show up as part of the workspace's change, burying
// the actual work under upstream history the user has never touched.
func resolveBase(root string, scope Scope) baseInfo {
	if scope == ScopeWorking {
		info := baseInfo{rev: "HEAD", label: "HEAD"}
		info.short, info.subject = describeCommit(root, "HEAD")
		return info
	}

	branch, err := git.GetBaseBranch(root)
	if err != nil || branch == "" {
		info := baseInfo{rev: "HEAD", label: "HEAD"}
		info.short, info.subject = describeCommit(root, "HEAD")
		return info
	}

	mergeBase := newestMergeBase(root, baseCandidates(root, branch))
	if mergeBase == "" {
		// No common ancestor could be found at all -- an unrelated history, or a
		// base branch that does not exist here. Diffing the branch tip is wrong
		// in a different way, but it is at least a diff the user can read.
		info := baseInfo{rev: branch, label: branch}
		info.short, info.subject = describeCommit(root, branch)
		return info
	}

	info := baseInfo{rev: mergeBase, label: branch}
	info.short, info.subject = describeCommit(root, mergeBase)
	return info
}

// baseCandidates lists the refs this branch could have been cut from or rebased
// onto, most likely first.
//
// Both are needed, because neither is right on its own. `git.GetBaseBranch`
// returns a *name*, so "main" resolves to the local branch -- which in a
// worktree may not have moved since the workspace was created, while the rebase
// went onto `origin/main`. And the local branch can equally be the newer of the
// two, when the user has pulled it and not the remote ref.
func baseCandidates(root, branch string) []string {
	var out []string
	for _, ref := range []string{"origin/" + branch, branch} {
		if git.ValidateRef(root, ref) == nil {
			out = append(out, ref)
		}
	}
	return out
}

// newestMergeBase returns the merge base furthest forward in history.
//
// Newest, not first: the whole question a review asks is "what has this
// workspace done", and of two candidate fork points the later one is the one
// that excludes more history the user did not write. Picking the older would
// reintroduce exactly the commits a rebase brought in.
func newestMergeBase(root string, refs []string) string {
	var best string
	for _, ref := range refs {
		out, err := git.RunGit(root, "--no-optional-locks", "merge-base", ref, "HEAD")
		if err != nil {
			continue
		}
		mergeBase := strings.TrimSpace(out)
		if mergeBase == "" {
			continue
		}
		if best == "" || isAncestor(root, best, mergeBase) {
			best = mergeBase
		}
	}
	return best
}

// isAncestor reports whether a is an ancestor of b.
//
// `merge-base --is-ancestor` answers by exit status, so any real failure -- an
// unresolvable ref, a git that could not run -- reads as "no" and leaves the
// candidate already chosen in place. That is the safe direction: it keeps the
// remote-tracking base, which is the one a rebase would have used.
func isAncestor(root, a, b string) bool {
	_, err := git.RunGit(root, "--no-optional-locks", "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// describeCommit returns a commit's short hash and subject, for the top bar.
//
// The base is worth showing because it is not something the user chose: it is
// derived, it moves under them on a rebase, and a review labelled only with a
// branch name gives them no way to tell which of those two things they are
// looking at.
func describeCommit(root, rev string) (short, subject string) {
	// Space-separated rather than NUL: a short hash cannot contain a space, so
	// the first one is an unambiguous boundary and nothing has to survive being
	// trimmed on the way back.
	out, err := git.RunGit(root, "--no-optional-locks", "log", "-1", "--format=%h %s", rev)
	if err != nil {
		return "", ""
	}
	short, subject, _ = strings.Cut(strings.TrimSpace(out), " ")
	return short, subject
}
