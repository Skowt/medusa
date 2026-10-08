package hooks

import (
	"path/filepath"
	"testing"
	"time"
)

// controlServer runs a hooks server and returns its directory and events.
func controlServer(t *testing.T) (string, <-chan HookEvent) {
	t.Helper()
	dir := shortTempDir(t)
	return dir, startTestServer(t, SocketPath(dir))
}

func nextEvent(t *testing.T, events <-chan HookEvent) HookEvent {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no event delivered")
		return HookEvent{}
	}
}

func TestNotifyWorkspacesChangedReachesTheServer(t *testing.T) {
	dir, events := controlServer(t)
	if !NotifyWorkspacesChanged(dir) {
		t.Fatal("NotifyWorkspacesChanged reported no listener")
	}
	if e := nextEvent(t, events); e.Event != EventWorkspacesChanged || e.SessionName != "" {
		t.Fatalf("event = %+v, want a session-less workspaces-changed", e)
	}
}

func TestNotifyWorkspaceClosingCarriesTheWorkspace(t *testing.T) {
	dir, events := controlServer(t)
	if !NotifyWorkspaceClosing(dir, "abc") {
		t.Fatal("NotifyWorkspaceClosing reported no listener")
	}
	if e := nextEvent(t, events); e.Event != EventWorkspaceClosing || e.WorkspaceID != "abc" {
		t.Fatalf("event = %+v, want workspace-closing for abc", e)
	}
}

func TestNotifyWithNoListenerReturnsFalseQuickly(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	if NotifyWorkspacesChanged(filepath.Join(dir, "nothing-here")) {
		t.Fatal("reported a listener where there is none")
	}
	if time.Since(start) > time.Second {
		t.Fatal("notify with no listener was slow")
	}
}

func TestHookEventsStillNeedASession(t *testing.T) {
	dir, events := controlServer(t)
	if !notify(dir, map[string]string{"event": string(EventStop)}) {
		t.Fatal("could not write to the socket")
	}
	select {
	case e := <-events:
		t.Fatalf("a session-less hook event was delivered: %+v", e)
	case <-time.After(200 * time.Millisecond):
	}
}
