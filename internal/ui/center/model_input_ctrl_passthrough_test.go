package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/vterm"
)

// Ctrl keys that used to be medusa shortcuts in an agent tab must now reach
// the agent: tab switching and closing live behind the prefix (ctrl+a n/p/x).
// Codex and Claude both bind several of them (ctrl+w deletes a word, ctrl+n/p
// walk history, ctrl+[ is Esc), so intercepting them took the key away from
// the agent.
//
// The tab's zero-value pty.Terminal fails every write, so a TabInputFailed
// from the returned command is the proof that the key went to the PTY.
func TestAgentTabForwardsFormerCtrlShortcuts(t *testing.T) {
	keys := map[string]tea.KeyPressMsg{
		"ctrl+n": {Code: 'n', Mod: tea.ModCtrl},
		"ctrl+p": {Code: 'p', Mod: tea.ModCtrl},
		"ctrl+w": {Code: 'w', Mod: tea.ModCtrl},
		"ctrl+]": {Code: ']', Mod: tea.ModCtrl},
		"ctrl+[": {Code: '[', Mod: tea.ModCtrl},
	}
	for name, msg := range keys {
		t.Run(name, func(t *testing.T) {
			m := newTestModelWithAgentTab(t)
			wt := m.workspace
			wtID := string(wt.ID())
			second := &Tab{
				ID:        TabID("tab-2"),
				Workspace: wt,
				Terminal:  vterm.New(80, 24),
				Running:   true,
			}
			m.tabsByWorkspace[wtID] = append(m.tabsByWorkspace[wtID], second)
			first := m.getTabs()[0]
			first.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}

			_, cmd := m.Update(msg)

			if got := m.getActiveTabIdx(); got != 0 {
				t.Errorf("%s switched tabs (active %d), want it forwarded", name, got)
			}
			if got := len(m.getTabs()); got != 2 {
				t.Errorf("%s changed the tab count to %d, want it forwarded", name, got)
			}
			if cmd == nil {
				t.Fatalf("%s returned no command, so it never reached the PTY", name)
			}
			if _, ok := cmd().(TabInputFailed); !ok {
				t.Errorf("%s was not written to the PTY", name)
			}
		})
	}
}

// The monitor grid used to scroll on ctrl+u/ctrl+d in a default-mode tab,
// which took line-kill and delete/exit away from the agent. PgUp/PgDn still
// scroll there, so these keys now go to the tile's PTY.
func TestMonitorTileForwardsCtrlUAndCtrlD(t *testing.T) {
	keys := map[string]tea.KeyPressMsg{
		"ctrl+u": {Code: 'u', Mod: tea.ModCtrl},
		"ctrl+d": {Code: 'd', Mod: tea.ModCtrl},
	}
	for name, msg := range keys {
		t.Run(name, func(t *testing.T) {
			m := newTestModelWithAgentTab(t)
			tab := m.getTabs()[0]
			tab.Agent = &appPty.Agent{Terminal: &appPty.Terminal{}}
			for i := 0; i < 50; i++ {
				tab.Terminal.Write([]byte("line\r\n"))
			}
			before := tab.Terminal.ViewOffset

			cmd := m.HandleMonitorInput(tab.ID, msg)

			if tab.Terminal.ViewOffset != before {
				t.Errorf("%s scrolled the tile (offset %d -> %d), want it forwarded", name, before, tab.Terminal.ViewOffset)
			}
			if cmd == nil {
				t.Fatalf("%s returned no command, so it never reached the PTY", name)
			}
			if _, ok := cmd().(TabInputFailed); !ok {
				t.Errorf("%s was not written to the PTY", name)
			}
		})
	}
}
