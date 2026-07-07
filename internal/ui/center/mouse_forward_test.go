package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
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
