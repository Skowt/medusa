package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// privateServer returns Options for a tmux server only this test uses, and
// kills it when the test ends.
func privateServer(t *testing.T) Options {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	opts := Options{ServerName: fmt.Sprintf("medusa-test-%d-%s", os.Getpid(), t.Name()), ConfigPath: "/dev/null"}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", opts.ServerName, "kill-server").Run()
	})
	return opts
}

func TestStartDetachedSessionCreatesATaggedSession(t *testing.T) {
	opts := privateServer(t)
	tags := SessionTags{WorkspaceID: "ws-1", TabID: "tab-1", Type: "agent", Assistant: "claude", CreatedAt: 42, Fullscreen: true}

	if err := StartDetachedSession("medusa-x-1", t.TempDir(), "sleep 30", []string{"FOO=bar"}, opts, tags); err != nil {
		t.Fatalf("StartDetachedSession: %v", err)
	}

	for key, want := range map[string]string{
		"@medusa":            "1",
		"@medusa_workspace":  "ws-1",
		"@medusa_tab":        "tab-1",
		"@medusa_type":       "agent",
		"@medusa_assistant":  "claude",
		"@medusa_fullscreen": "1",
	} {
		got, err := SessionTagValue("medusa-x-1", key, opts)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, err, want)
		}
	}
	state, err := SessionStateFor("medusa-x-1", opts)
	if err != nil || !state.Exists || !state.HasLivePane {
		t.Fatalf("session state = %+v, %v; want a live session", state, err)
	}
	if n, err := SessionClientCount("medusa-x-1", opts); err != nil || n != 0 {
		t.Fatalf("SessionClientCount = %d, %v; want 0 for a detached session", n, err)
	}
}

func TestStartDetachedSessionRefusesAnExistingName(t *testing.T) {
	opts := privateServer(t)
	dir := t.TempDir()
	if err := StartDetachedSession("dup", dir, "sleep 30", nil, opts, SessionTags{}); err != nil {
		t.Fatal(err)
	}
	if err := StartDetachedSession("dup", dir, "sleep 30", nil, opts, SessionTags{}); err == nil {
		t.Fatal("a second session with the same name was accepted")
	}
}

func TestSessionClientCountOfAMissingSessionIsZero(t *testing.T) {
	opts := privateServer(t)
	if n, err := SessionClientCount("nope", opts); err != nil || n != 0 {
		t.Fatalf("SessionClientCount(missing) = %d, %v; want 0, nil", n, err)
	}
}
