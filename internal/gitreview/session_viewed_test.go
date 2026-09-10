package gitreview

import (
	"testing"
)

// viewedSession returns a session over a real repo with its first snapshot read,
// which is what SetViewed validates paths against.
func viewedSession(t *testing.T, root string) *Session {
	t.Helper()
	s := &Session{
		root: root, workspace: "demo", scope: ScopeBranch,
		threads: []*Thread{}, viewed: map[string]string{},
		subs: make(map[int]chan Event),
	}
	s.Refresh(ScopeBranch)
	return s
}

func hasPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// TestViewedTickClearsWhenTheFileChanges is the property the whole feature rests
// on. A tick is stored against the digest of the diff that was on screen, so an
// agent rewriting the file takes the tick away with it -- a page still claiming
// the reader has seen code they have never seen is worse than one that never
// offered to remember.
func TestViewedTickClearsWhenTheFileChanges(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	s := viewedSession(t, root)

	if !s.SetViewed("keep.txt", true) {
		t.Fatal("SetViewed refused a path that is in the diff")
	}
	if !hasPath(s.ViewedPaths(), "keep.txt") {
		t.Fatalf("keep.txt was not marked viewed: %v", s.ViewedPaths())
	}

	write(t, root, "keep.txt", "one\nTWO\nthree\nfour\n")
	s.Refresh(ScopeBranch)

	if hasPath(s.ViewedPaths(), "keep.txt") {
		t.Error("the tick survived a change to the file it was made against")
	}
}

// TestViewedTickSurvivesAChangeElsewhere is the other half: invalidation has to
// be per file, or one agent edit anywhere would wipe the reader's whole progress.
func TestViewedTickSurvivesAChangeElsewhere(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	s := viewedSession(t, root)

	s.SetViewed("keep.txt", true)
	write(t, root, "committed.txt", "alpha\nBETA\ngamma\n")
	s.Refresh(ScopeBranch)

	if !hasPath(s.ViewedPaths(), "keep.txt") {
		t.Error("editing another file cleared keep.txt's tick")
	}
}

// TestViewedTickCanBeCleared covers unticking.
func TestViewedTickCanBeCleared(t *testing.T) {
	skipIfNoGit(t)
	s := viewedSession(t, branchRepo(t))

	s.SetViewed("keep.txt", true)
	if !s.SetViewed("keep.txt", false) {
		t.Fatal("clearing a tick was refused")
	}
	if len(s.ViewedPaths()) != 0 {
		t.Errorf("tick was not cleared: %v", s.ViewedPaths())
	}
}

// TestSetViewedRefusesAPathNotInTheDiff keeps the map to real files. It is also
// what makes a traversal guard unnecessary: the only paths that get in are ones
// the snapshot itself produced.
func TestSetViewedRefusesAPathNotInTheDiff(t *testing.T) {
	skipIfNoGit(t)
	s := viewedSession(t, branchRepo(t))

	for _, path := range []string{"nope.txt", "../../.ssh/config", ""} {
		if s.SetViewed(path, true) {
			t.Errorf("SetViewed accepted %q", path)
		}
	}
	if len(s.ViewedPaths()) != 0 {
		t.Errorf("a refused path was recorded anyway: %v", s.ViewedPaths())
	}
}

// TestViewedMarksPersist: review progress is the same argument as the comments'
// -- a review spanning two sittings that forgets what was already read is worse
// than one that never offered to remember.
func TestViewedMarksPersist(t *testing.T) {
	meta := t.TempDir()
	st := &store{dir: meta}

	st.save("ws", "/root", []*Thread{}, map[string]string{"a.go": "digest-1"})

	_, viewed := st.load("ws", "/root")
	if viewed["a.go"] != "digest-1" {
		t.Errorf("viewed marks did not round trip: %+v", viewed)
	}
}

// TestViewedIsSavedAgainstTheDigestNotABool guards the on-disk shape. Stored as
// a bool, a reload would tick files whose diffs had moved on since.
func TestViewedIsSavedAgainstTheDigestNotABool(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	meta := t.TempDir()

	s := viewedSession(t, root)
	s.workspaceID, s.store = "ws", &store{dir: meta}
	s.SetViewed("keep.txt", true)

	_, viewed := s.store.load("ws", root)
	if viewed["keep.txt"] == "" {
		t.Fatalf("no mark was persisted: %+v", viewed)
	}

	// A fresh session over a changed file must not honour the saved mark.
	write(t, root, "keep.txt", "one\nTWO\nthree\nfour\n")
	fresh := &Session{
		root: root, workspace: "demo", scope: ScopeBranch,
		threads: []*Thread{}, viewed: viewed,
		subs: make(map[int]chan Event),
	}
	fresh.Refresh(ScopeBranch)
	if hasPath(fresh.ViewedPaths(), "keep.txt") {
		t.Error("a persisted mark was honoured after the file changed")
	}
}

// TestCommentWithNoEndLineStaysOnItsOwnLine: an API client that posts only
// startLine used to get a comment anchored from line 0, which the page now draws
// as a marked run from the top of the file.
func TestCommentWithNoEndLineStaysOnItsOwnLine(t *testing.T) {
	c := NewComment{Path: "a.go", StartLine: 10, Body: "x"}
	if !c.normalize() {
		t.Fatal("normalize rejected a valid comment")
	}
	if c.StartLine != 10 || c.EndLine != 10 {
		t.Errorf("start/end = %d/%d, want 10/10", c.StartLine, c.EndLine)
	}

	// A genuinely reversed range is still corrected.
	rev := NewComment{Path: "a.go", StartLine: 12, EndLine: 4, Body: "x"}
	rev.normalize()
	if rev.StartLine != 4 || rev.EndLine != 12 {
		t.Errorf("reversed range came out %d/%d, want 4/12", rev.StartLine, rev.EndLine)
	}
}
