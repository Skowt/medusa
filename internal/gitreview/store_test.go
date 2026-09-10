package gitreview

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestCommentsSurviveARestart is the whole point of persisting. A sent thread is
// work an agent was told about and may still be acting on; a draft is something
// the user typed. Losing either to a restart makes the page untrustworthy for
// any review that takes more than one sitting.
func TestCommentsSurviveARestart(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	meta := t.TempDir()

	first := NewService(meta, nil)
	if _, err := first.Open(OpenRequest{
		WorkspaceID: "ws-abc", Root: root, Name: "demo", Scope: ScopeBranch,
	}); err != nil {
		t.Fatal(err)
	}
	session := first.Sessions()[0]
	added := session.AddComments([]NewComment{
		{Path: "keep.txt", Side: SideNew, StartLine: 2, EndLine: 4, Quote: []string{"+TWO"}, Body: "draft comment"},
		{Path: "committed.txt", Side: SideNew, StartLine: 2, Body: "sent comment"},
	})
	session.MarkSent([]string{added[1].ID}, "")
	session.AddReply(added[1].ID, AuthorAgent, "done", true)
	_ = first.Close()

	// A second Service is what a restarted medusa looks like.
	second := NewService(meta, nil)
	defer func() { _ = second.Close() }()
	if _, err := second.Open(OpenRequest{
		WorkspaceID: "ws-abc", Root: root, Name: "demo", Scope: ScopeBranch,
	}); err != nil {
		t.Fatal(err)
	}
	restored := second.Sessions()[0].Threads()

	if len(restored) != 2 {
		t.Fatalf("restored %d threads, want 2", len(restored))
	}
	byBody := map[string]Thread{}
	for _, thread := range restored {
		byBody[thread.Body] = thread
	}

	draft, ok := byBody["draft comment"]
	if !ok {
		t.Fatal("the draft comment did not survive")
	}
	if draft.Sent {
		t.Error("a draft came back marked sent")
	}
	if draft.StartLine != 2 || draft.EndLine != 4 {
		t.Errorf("draft range = %d-%d, want 2-4", draft.StartLine, draft.EndLine)
	}
	if len(draft.Quote) != 1 || draft.Quote[0] != "+TWO" {
		t.Errorf("draft lost its quote: %v", draft.Quote)
	}

	sent, ok := byBody["sent comment"]
	if !ok {
		t.Fatal("the sent comment did not survive")
	}
	if !sent.Sent {
		t.Error("a sent thread came back as a draft, so it would be submitted twice")
	}
	if !sent.Resolved {
		t.Error("resolved state did not survive")
	}
	if len(sent.Replies) != 1 || sent.Replies[0].Body != "done" {
		t.Errorf("the agent's reply did not survive: %+v", sent.Replies)
	}
}

// TestCommentsAreFiledUnderTheWorkspaceID keeps them beside the rest of that
// workspace's metadata rather than in a directory nothing else knows about.
func TestCommentsAreFiledUnderTheWorkspaceID(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	meta := t.TempDir()

	service := NewService(meta, nil)
	defer func() { _ = service.Close() }()
	if _, err := service.Open(OpenRequest{WorkspaceID: "ws-xyz", Root: root, Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	service.Sessions()[0].AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "hi"}})

	want := filepath.Join(meta, "ws-xyz", commentsFilename)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("comments not written to %s: %v", want, err)
	}
}

// TestPersistenceWithoutAWorkspaceIDStillWorks: a review opened without an id
// must not silently stop saving.
func TestPersistenceWithoutAWorkspaceIDStillWorks(t *testing.T) {
	meta := t.TempDir()
	s := &store{dir: meta}

	threads := []*Thread{{ID: "t1", Path: "a.txt", Body: "x", CreatedAt: time.Now()}}
	s.save("", "/some/root", threads, nil)

	if got, _ := s.load("", "/some/root"); len(got) != 1 || got[0].ID != "t1" {
		t.Errorf("round trip without a workspace id lost the thread: %+v", got)
	}
	// A different root must not read the same file.
	if got, _ := s.load("", "/other/root"); len(got) != 0 {
		t.Errorf("comments leaked across roots: %+v", got)
	}
}

// TestStoreWithNoDirIsANoOp keeps a Service built without a metadata root
// working instead of erroring on every comment.
func TestStoreWithNoDirIsANoOp(t *testing.T) {
	s := &store{}
	s.save("ws", "/root", []*Thread{{ID: "t1"}}, nil)
	if got, _ := s.load("ws", "/root"); got != nil {
		t.Errorf("a store with no dir returned %+v", got)
	}
}

// TestCorruptStoreDoesNotBreakTheReview: a half-written or hand-edited file must
// degrade to "no history", never take the page down.
func TestCorruptStoreDoesNotBreakTheReview(t *testing.T) {
	meta := t.TempDir()
	s := &store{dir: meta}
	path := s.path("ws", "/root")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.load("ws", "/root"); got != nil {
		t.Errorf("corrupt file produced %+v", got)
	}
	// And the file is left alone, so the user can recover it by hand.
	if _, err := os.Stat(path); err != nil {
		t.Error("a corrupt comments file was deleted rather than left for recovery")
	}
}

// TestPruneDropsResolvedBeforeUnresolved: an unresolved comment is outstanding
// work and is the last thing that should quietly disappear.
func TestPruneDropsResolvedBeforeUnresolved(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	var threads []*Thread
	// Oldest first: resolved ones early, so age alone would drop them anyway;
	// the unresolved ones are also old, which is what makes the ordering matter.
	for i := 0; i < maxPersistedThreads; i++ {
		threads = append(threads, &Thread{
			ID:        "resolved-" + strconv.Itoa(i),
			Resolved:  true,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	for i := 0; i < 10; i++ {
		threads = append(threads, &Thread{
			ID:        "open-" + strconv.Itoa(i),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	kept := prune(threads)
	if len(kept) != maxPersistedThreads {
		t.Fatalf("kept %d, want %d", len(kept), maxPersistedThreads)
	}
	open := 0
	for _, thread := range kept {
		if !thread.Resolved {
			open++
		}
	}
	if open != 10 {
		t.Errorf("kept %d unresolved threads, want all 10", open)
	}
}

// TestPruneKeepsOrder: the live list's order is what the page renders.
func TestPruneKeepsOrder(t *testing.T) {
	var threads []*Thread
	for i := 0; i < maxPersistedThreads+5; i++ {
		threads = append(threads, &Thread{ID: strconv.Itoa(i), CreatedAt: time.Now().Add(time.Duration(i) * time.Second)})
	}
	kept := prune(threads)
	for i := 1; i < len(kept); i++ {
		if kept[i-1].CreatedAt.After(kept[i].CreatedAt) {
			t.Fatal("prune reordered the thread list")
		}
	}
}

// TestPruneLeavesSmallListsAlone avoids copying on the common path.
func TestPruneLeavesSmallListsAlone(t *testing.T) {
	threads := []*Thread{{ID: "a"}, {ID: "b"}}
	if got := prune(threads); len(got) != 2 {
		t.Errorf("prune changed a short list: %+v", got)
	}
}
