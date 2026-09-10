package gitreview

import (
	"strings"
	"testing"
)

// rebasedRepo builds the situation the base resolution exists for: a workspace
// branch rebased onto a moved remote base, with the *local* base branch left
// where it was when the worktree was cut.
//
// That is the ordinary shape of a medusa worktree. `git.GetBaseBranch` returns a
// branch *name*, so "main" resolves to that stale local ref, and its merge base
// with HEAD is the old fork point -- which drags every commit the rebase brought
// in into the review.
func rebasedRepo(t *testing.T) string {
	t.Helper()
	skipIfNoGit(t)
	root := t.TempDir()

	runGit(t, root, "init", "-b", "main")
	write(t, root, "shared.txt", "one\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "fork point")

	// A fake remote base, moved on by a commit the user never wrote.
	runGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, root, "checkout", "-b", "upstream-work")
	write(t, root, "upstream.txt", "from someone else\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "upstream commit")
	runGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")

	// Our branch, cut from the old fork point and then rebased onto the new
	// remote base -- exactly what happens mid-session.
	runGit(t, root, "checkout", "-b", "feature", "origin/main")
	write(t, root, "mine.txt", "my work\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "my commit")

	// The local `main` never moved.
	runGit(t, root, "branch", "-f", "main", "origin/main~1")
	return root
}

// TestBranchScopeShowsOnlyOurWorkAfterARebase is the reported bug. The rebase
// brought in a commit the user never wrote, and a merge base taken against the
// stale local branch put that commit in their review.
func TestBranchScopeShowsOnlyOurWorkAfterARebase(t *testing.T) {
	root := rebasedRepo(t)

	snap := Build(root, "ws", ScopeBranch)
	if snap.Error != "" {
		t.Fatalf("build: %s", snap.Error)
	}

	paths := map[string]bool{}
	for _, f := range snap.Files {
		paths[f.Path] = true
	}
	if !paths["mine.txt"] {
		t.Errorf("the workspace's own change is missing; got %v", keys(pathSet(snap)))
	}
	if paths["upstream.txt"] {
		t.Errorf("a commit the rebase brought in is being shown as our change; got %v", keys(pathSet(snap)))
	}
}

// TestNewestMergeBaseWins pins the rule directly: of two candidate fork points,
// the later one is the one that excludes more history the user did not write.
func TestNewestMergeBaseWins(t *testing.T) {
	root := rebasedRepo(t)

	base := resolveBase(root, ScopeBranch)
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD~1"))
	if base.rev != head {
		t.Errorf("base = %s, want the rebase target %s", base.rev, head)
	}
	// And the older candidate is genuinely a different commit, or this test
	// would pass without the rule doing anything.
	older := strings.TrimSpace(runGit(t, root, "merge-base", "main", "HEAD"))
	if older == base.rev {
		t.Fatal("the two candidates agree, so this case proves nothing")
	}
	if !isAncestor(root, older, base.rev) {
		t.Error("the chosen base is not the newer of the two")
	}
}

// TestBaseIsNamedByCommitNotOnlyByBranch covers what the page shows. The base is
// derived and moves under the reader, so a review labelled only "main" gives
// them no way to tell which fork point they are looking at.
func TestBaseIsNamedByCommitNotOnlyByBranch(t *testing.T) {
	root := rebasedRepo(t)

	snap := Build(root, "ws", ScopeBranch)
	if snap.Base != "main" {
		t.Errorf("base label = %q, want main", snap.Base)
	}
	if snap.BaseRev == "" {
		t.Error("no base commit was reported, so the page cannot show which one it is")
	}
	if snap.BaseSubject != "upstream commit" {
		t.Errorf("base subject = %q, want %q", snap.BaseSubject, "upstream commit")
	}

	// The working scope describes HEAD, for the same reason.
	work := Build(root, "ws", ScopeWorking)
	if work.BaseRev == "" || work.BaseSubject != "my commit" {
		t.Errorf("working scope described its base as %q %q", work.BaseRev, work.BaseSubject)
	}
}

// TestTheBaseIsReResolvedOnEveryRefresh: held from when the page opened, a
// rebase mid-session would leave the review measuring from the old fork point.
func TestTheBaseIsReResolvedOnEveryRefresh(t *testing.T) {
	root := rebasedRepo(t)
	session := &Session{root: root, workspace: "demo", scope: ScopeBranch, subs: make(map[int]chan Event)}

	first := session.Refresh(ScopeBranch)
	if first.BaseRev == "" {
		t.Fatal("no base on the first refresh")
	}

	// Rebase again, onto a further upstream commit.
	runGit(t, root, "checkout", "-q", "origin/main")
	write(t, root, "upstream2.txt", "more from someone else\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "second upstream commit")
	runGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, root, "checkout", "-q", "feature")
	runGit(t, root, "rebase", "-q", "origin/main")

	second := session.Refresh(ScopeBranch)
	if second.BaseRev == first.BaseRev {
		t.Error("the base did not move after a rebase, so it is being held from open time")
	}
	for _, f := range second.Files {
		if strings.HasPrefix(f.Path, "upstream") {
			t.Errorf("the second rebase's commits leaked into the review: %s", f.Path)
		}
	}
	if second.Digest == first.Digest {
		t.Error("the snapshot digest did not move with the base, so the page would not repaint")
	}
}

// pathSet is a small helper for the failure messages above.
func pathSet(snap Snapshot) map[string]File {
	out := map[string]File{}
	for _, f := range snap.Files {
		out[f.Path] = f
	}
	return out
}
