package gitreview

import (
	"sync"
	"testing"
	"time"
)

// TestPollerPushesTheAgentsNextEdit is the property the whole live view rests
// on: the agent edits the files with no signal this package can see, so the
// page has to be told by a poller that noticed the diff move.
func TestPollerPushesTheAgentsNextEdit(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	service := NewService(t.TempDir(), nil)
	defer func() { _ = service.Close() }()
	if _, err := service.Open(OpenRequest{Root: root, Name: "demo", AgentSession: "agent", Scope: ScopeBranch}); err != nil {
		t.Fatal(err)
	}
	session := service.Sessions()[0]

	// Subscribing is what marks the session watched, which is what makes the
	// poller pick it up.
	events, unsubscribe := session.subscribe()
	defer unsubscribe()

	// Stand in for the agent's next edit.
	write(t, root, "keep.txt", "one\nTWO\nthree\nfour added by the agent\n")

	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type != EventSnapshot || ev.Snapshot == nil {
				continue
			}
			for _, f := range ev.Snapshot.Files {
				if f.Path != "keep.txt" {
					continue
				}
				for _, hunk := range f.Hunks {
					for _, line := range hunk.Lines {
						if line.Content == "+four added by the agent" {
							return // the page would have repainted
						}
					}
				}
			}
		case <-deadline:
			t.Fatal("the poller never pushed the agent's edit to the page")
		}
	}
}

// TestPollerLeavesUnwatchedSessionsAlone keeps a review whose page has been
// closed from running git over the repository forever.
func TestPollerLeavesUnwatchedSessionsAlone(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	service := NewService(t.TempDir(), nil)
	defer func() { _ = service.Close() }()
	if _, err := service.Open(OpenRequest{Root: root, Name: "demo", AgentSession: "agent", Scope: ScopeBranch}); err != nil {
		t.Fatal(err)
	}
	session := service.Sessions()[0]

	if session.watched() {
		t.Fatal("a session with no page open reports as watched")
	}
	if got := len(service.watchedSessions()); got != 0 {
		t.Errorf("watchedSessions = %d with no page open, want 0", got)
	}

	_, unsubscribe := session.subscribe()
	if got := len(service.watchedSessions()); got != 1 {
		t.Errorf("watchedSessions = %d with a page open, want 1", got)
	}
	unsubscribe()
	if got := len(service.watchedSessions()); got != 0 {
		t.Errorf("watchedSessions = %d after the page closed, want 0", got)
	}
}

// TestBroadcastSurvivesAStalledClient is why the fan-out drops instead of
// blocking: the submit path broadcasts too, and one browser tab that has stopped
// reading must not be able to wedge the user's own submission.
func TestBroadcastSurvivesAStalledClient(t *testing.T) {
	session := &Session{subs: make(map[int]chan Event)}
	stalled, unsubscribe := session.subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < liveBuffer*4; i++ {
			session.broadcast(Event{Type: EventNotice, Notice: "x"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("broadcast blocked on a client that stopped reading")
	}
	// The client is still registered and still readable — it lost events, which
	// is safe because every event carries the whole state it describes.
	if len(stalled) == 0 {
		t.Error("the stalled client received nothing at all")
	}
}

// TestScopeChangeAlwaysRepaints: the digest covers the diff's content, so two
// scopes that happen to produce the same diff would otherwise leave the page
// labelled with a scope it is not showing.
func TestScopeChangeAlwaysRepaints(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	write(t, root, "a.txt", "one\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")
	write(t, root, "a.txt", "two\n")

	session := &Session{root: root, workspace: "demo", scope: ScopeBranch, subs: make(map[int]chan Event)}
	session.Refresh(ScopeBranch)

	events, unsubscribe := session.subscribe()
	defer unsubscribe()

	// On a repo with no branch divergence both scopes produce the same diff, so
	// only the scope-change rule can make this repaint.
	session.Refresh(ScopeWorking)

	select {
	case ev := <-events:
		if ev.Snapshot == nil || ev.Snapshot.Scope != ScopeWorking {
			t.Errorf("event did not carry the new scope: %+v", ev.Snapshot)
		}
	default:
		t.Error("a scope change did not repaint the page")
	}
}

// TestBroadcastRacesUnsubscribe guards a crash, not a glitch. Sending on a
// closed channel is a panic, and a page closing closes its channel — so with
// the subscriber list copied and the lock released, a review tab closed while
// the poller broadcast took the whole TUI process down with it.
//
// The fix is that broadcast holds the lock across its sends, which is only
// affordable because every send is non-blocking.
func TestBroadcastRacesUnsubscribe(t *testing.T) {
	session := &Session{subs: make(map[int]chan Event)}
	var wg sync.WaitGroup

	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, unsubscribe := session.subscribe()
			unsubscribe()
		}()
	}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				session.broadcast(Event{Type: EventNotice, Notice: "x"})
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentThreadWritesAreSerialized covers the other side of the same
// surface: the agent replies over HTTP while the user comments in the browser
// and the poller refreshes, all on different goroutines.
func TestConcurrentThreadWritesAreSerialized(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	session := &Session{root: root, workspace: "demo", scope: ScopeBranch, subs: make(map[int]chan Event)}

	threads := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "start"}})
	id := threads[0].ID

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.AddReply(id, AuthorAgent, "working on it", false)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 3, Body: "more"}})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.Threads()
		}()
	}
	wg.Wait()

	if got := len(session.Threads()); got != 21 {
		t.Errorf("threads = %d, want 21", got)
	}
}

// TestPollerPushesANewlyCreatedFile covers the case an agent hits constantly:
// a file it has just written exists in no diff and no commit, so it reaches the
// page only if untracked files are re-enumerated on every refresh rather than
// resolved once when the review opened.
func TestPollerPushesANewlyCreatedFile(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	session := &Session{root: root, workspace: "demo", scope: ScopeBranch, subs: make(map[int]chan Event)}
	session.Refresh(ScopeBranch)

	events, unsubscribe := session.subscribe()
	defer unsubscribe()

	write(t, root, "internal/new_from_agent.go", "package internal\n\nfunc Added() {}\n")
	session.Refresh(ScopeBranch)

	select {
	case ev := <-events:
		if ev.Snapshot == nil {
			t.Fatal("event carried no snapshot")
		}
		for _, f := range ev.Snapshot.Files {
			if f.Path == "internal/new_from_agent.go" {
				if !f.Untracked {
					t.Error("a brand new file should be flagged untracked")
				}
				if len(f.Hunks) == 0 {
					t.Error("a brand new file should still have reviewable rows")
				}
				return
			}
		}
		t.Errorf("a newly created file never reached the page: %v", pathsOf(ev.Snapshot.Files))
	default:
		t.Fatal("creating a file did not repaint the page")
	}
}

func pathsOf(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

// TestWakeRefreshesImmediately covers the host's file-watcher poke. It exists so
// a commit or a stage repaints at once instead of waiting out a tick.
func TestWakeRefreshesImmediately(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	service := NewService(t.TempDir(), nil)
	defer func() { _ = service.Close() }()
	if _, err := service.Open(OpenRequest{Root: root, Name: "demo", Scope: ScopeBranch}); err != nil {
		t.Fatal(err)
	}
	session := service.Sessions()[0]
	events, unsubscribe := session.subscribe()
	defer unsubscribe()
	drain(events)

	write(t, root, "keep.txt", "one\nTWO\nthree\nwoken\n")
	service.NotifyRootChanged(root)

	// Well inside a tick, so only the wake can account for this arriving.
	select {
	case ev := <-events:
		if ev.Snapshot == nil {
			t.Fatal("wake produced an event with no snapshot")
		}
	case <-time.After(pollInterval - 500*time.Millisecond):
		t.Fatal("a watcher poke did not refresh the page ahead of the next tick")
	}
}

// TestNotifyRootChangedIsSafeWhenIdle keeps the UI thread's poke harmless before
// anything is serving and for repositories with no review open.
func TestNotifyRootChangedIsSafeWhenIdle(t *testing.T) {
	service := NewService(t.TempDir(), nil)
	defer func() { _ = service.Close() }()

	// Never served, so there is no poll goroutine to receive this.
	service.NotifyRootChanged("/no/such/root")
	service.NotifyRootChanged("")
}

// TestNotifyRootChangedNeverBlocks is why the send is non-blocking: it is called
// from the UI thread, and a full queue must not stall the interface.
func TestNotifyRootChangedNeverBlocks(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	service := NewService(t.TempDir(), nil)
	defer func() { _ = service.Close() }()
	if _, err := service.Open(OpenRequest{Root: root, Name: "demo", Scope: ScopeBranch}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < wakeBuffer*10; i++ {
			service.NotifyRootChanged(root)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("NotifyRootChanged blocked once its queue filled")
	}
}

// drain empties any events already queued, so a test can assert on what comes
// next rather than on the opening state.
func drain(events <-chan Event) {
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}
