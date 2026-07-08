package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/vterm"
)

// newTestModelWithAgentTab builds a Model with one focused, active agent tab
// suitable for exercising mouse-forwarding routing. Agent is left nil so
// forwardMouse is a safe no-op while the routing branch is still exercised.
func newTestModelWithAgentTab(t *testing.T) *Model {
	t.Helper()
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	wt := data.NewWorkspace("wt", "", "", "/tmp/repo", "/tmp/repo")
	m.SetWorkspace(wt)
	wtID := string(wt.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Workspace: wt,
		Terminal:  vterm.New(80, 24),
		Agent:     nil,
		Running:   true,
	}
	m.tabsByWorkspace[wtID] = []*Tab{tab}
	m.activeTabByWorkspace[wtID] = 0
	m.SetSize(100, 40)
	m.SetOffset(0)
	m.Focus()
	return m
}

func TestEncodeSGRMouse(t *testing.T) {
	if got := string(encodeSGRMouse(0, 10, 5, false)); got != "\x1b[<0;10;5M" {
		t.Errorf("press encoding wrong: %q", got)
	}
	if got := string(encodeSGRMouse(0, 10, 5, true)); got != "\x1b[<0;10;5m" {
		t.Errorf("release encoding wrong: %q", got)
	}
	if got := string(encodeSGRMouse(64, 1, 1, false)); got != "\x1b[<64;1;1M" {
		t.Errorf("wheel-up encoding wrong: %q", got)
	}
}

func TestSGRMouseButton(t *testing.T) {
	cases := []struct {
		b      tea.MouseButton
		motion bool
		want   int
		ok     bool
	}{
		{tea.MouseLeft, false, 0, true},
		{tea.MouseMiddle, false, 1, true},
		{tea.MouseRight, false, 2, true},
		{tea.MouseWheelUp, false, 64, true},
		{tea.MouseWheelDown, false, 65, true},
		{tea.MouseLeft, true, 32, true}, // drag: motion bit (+32)
		{tea.MouseNone, false, 0, false},
	}
	for _, c := range cases {
		got, ok := sgrMouseButton(c.b, c.motion)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("button %v motion=%v => (%d,%v), want (%d,%v)", c.b, c.motion, got, ok, c.want, c.ok)
		}
	}
}

func TestFullscreenTabDoesNotScrollVterm(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	// Give the vterm scrollback so ScrollView would visibly move it.
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = true
	before := tab.Terminal.ViewOffset

	m.updateMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if tab.Terminal.ViewOffset != before {
		t.Errorf("fullscreen tab must not scroll medusa vterm (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}

func TestClassicTabScrollsVterm(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	before := tab.Terminal.ViewOffset

	m.updateMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if tab.Terminal.ViewOffset == before {
		t.Errorf("classic tab must scroll medusa vterm (offset unchanged at %d)", before)
	}
}

// TestFullscreenTabDoesNotScrollOnPgUp and TestClassicTabScrollsOnPgUp drive
// PgUp through the real Model.Update entry point (there is no standalone
// key-handling method to call the way updateMouseWheel exists for wheel
// events; Update's tea.KeyPressMsg case owns this logic directly). Reaching
// the PgUp/PgDown switch requires tab.Agent/tab.Agent.Terminal to be
// non-nil, so unlike newTestModelWithAgentTab's default (nil Agent, which is
// fine for mouse-forward routing tests), these tests attach a zero-value
// *pty.Terminal: its SendString always errors (no real ptyFile), which is
// harmless here since a successful PgUp/PgDown scroll returns before ever
// reaching the "forward key to terminal" code path.
func TestFullscreenTabDoesNotScrollOnPgUp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	tab.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = true
	before := tab.Terminal.ViewOffset

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	if tab.Terminal.ViewOffset != before {
		t.Fatalf("fullscreen tab must not scroll its vterm on PgUp (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}

func TestClassicTabScrollsOnPgUp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	tab.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	before := tab.Terminal.ViewOffset

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	if tab.Terminal.ViewOffset == before {
		t.Fatalf("classic tab must scroll its vterm on PgUp")
	}
}

// TestFullscreenTabDoesNotScrollOnMonitorPgUp and
// TestClassicTabScrollsOnMonitorPgUp exercise the monitor-view scroll gate
// (HandleMonitorInput) the same way TestFullscreenTabDoesNotScrollOnPgUp and
// TestClassicTabScrollsOnPgUp exercise the main-view gate above: a fullscreen
// tab must not have its medusa vterm scrolled by monitor PgUp — that key must
// fall through to the PTY-forward tail and reach Claude instead.
func TestFullscreenTabDoesNotScrollOnMonitorPgUp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	tab.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = true
	before := tab.Terminal.ViewOffset

	_ = m.HandleMonitorInput(tab.ID, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if tab.Terminal.ViewOffset != before {
		t.Fatalf("fullscreen tab must not scroll its vterm on monitor PgUp (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}

func TestClassicTabScrollsOnMonitorPgUp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	tab.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	before := tab.Terminal.ViewOffset

	_ = m.HandleMonitorInput(tab.ID, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if tab.Terminal.ViewOffset == before {
		t.Fatalf("classic tab must scroll its vterm on monitor PgUp")
	}
}

// A classic-launched tab whose app switched to the alt screen and enabled
// mouse reporting at runtime (e.g. `/tui fullscreen` inside Claude Code) must
// hand wheel input to the app instead of scrolling medusa's vterm — otherwise
// the view scrolls into stale capture/frame-fragment scrollback the app knows
// nothing about.
func TestClassicTabAltScreenMouseAppForwardsWheel(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	tab.Terminal.Write([]byte("\x1b[?1049h\x1b[?1003h\x1b[?1006h"))
	before := tab.Terminal.ViewOffset

	if _, ok := m.activeTabForwardsMouse(); !ok {
		t.Fatal("alt-screen app with mouse reporting should receive forwarded mouse input")
	}
	m.updateMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if tab.Terminal.ViewOffset != before {
		t.Errorf("wheel must not scroll medusa vterm while the app owns the alt screen (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}

// Without mouse reporting (e.g. plain vim/less), wheel keeps scrolling
// medusa's vterm view even in the alt screen.
func TestClassicTabAltScreenNoMouseStillScrollsVterm(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	tab.Terminal.Write([]byte("\x1b[?1049h"))
	before := tab.Terminal.ViewOffset

	if _, ok := m.activeTabForwardsMouse(); ok {
		t.Fatal("alt-screen app without mouse reporting should not receive forwarded mouse input")
	}
	m.updateMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if tab.Terminal.ViewOffset == before {
		t.Errorf("wheel should scroll medusa vterm when the app did not request the mouse")
	}
}

// PgUp on a classic tab whose app is in the alt screen must go to the app
// (fall through to key forwarding), not scroll medusa's vterm.
func TestClassicTabAltScreenPgUpGoesToApp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	tab.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	tab.Terminal.Write([]byte("\x1b[?1049h"))
	before := tab.Terminal.ViewOffset

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	if tab.Terminal.ViewOffset != before {
		t.Fatalf("PgUp must not scroll medusa vterm while the app owns the alt screen (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}
