package app

import (
	"os"
	"strings"
	"testing"
)

// TestGitStatusRefreshIsNotGatedOnTheSidebar guards a refresh that has more
// than one consumer. Both paths that request git status for the active
// workspace used to skip it while the sidebar was hidden, back when the sidebar
// was the only thing reading it. The dashboard's per-workspace change
// indicators read it too, so gating on the sidebar leaves them frozen for
// anyone who works with it collapsed.
func TestGitStatusRefreshIsNotGatedOnTheSidebar(t *testing.T) {
	source, err := os.ReadFile("app_input_pty.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"handleGitStatusTick", "handleFileWatcherEvent"} {
		body := functionBody(t, string(source), fn)
		if strings.Contains(body, "SidebarHidden") {
			t.Errorf("%s still gates the git status request on the sidebar; "+
				"the dashboard indicators need it regardless", fn)
		}
	}
}

// functionBody returns the source of a top-level function, up to the closing
// brace in column zero.
func functionBody(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "func (a *App) "+name+"(")
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("could not find the end of %s", name)
	}
	return source[start : start+end]
}
