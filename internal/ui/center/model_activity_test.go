package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/hooks"
)

func newTestModel() *Model {
	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"claude": {},
			"codex":  {},
		},
	}
	return New(cfg)
}

func newTestWorkspace(name, root string) *data.Workspace {
	return data.NewWorkspace(name, "", "", root, root)
}

func TestIsTabActiveChatOnly(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	activeChat := &Tab{
		Assistant: "claude", Workspace: ws, Running: true,
		HookState: string(hooks.EventPreToolUse),
	}
	m.tabsByWorkspace[string(ws.ID())] = []*Tab{activeChat}

	if !m.IsTabActive(activeChat) {
		t.Fatalf("expected chat tab to be active with recent output")
	}
}

func TestIsTabActiveIgnoresDetachedAndNonChat(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	nonChat := &Tab{
		Assistant: "vim", Workspace: ws, Running: true,
		HookState: string(hooks.EventPreToolUse),
	}
	if m.IsTabActive(nonChat) {
		t.Fatalf("expected non-chat tab to be inactive even with output")
	}

	detached := &Tab{
		Assistant: "claude", Workspace: ws, Running: true, Detached: true,
		HookState: string(hooks.EventPreToolUse),
	}
	if m.IsTabActive(detached) {
		t.Fatalf("expected detached chat tab to be inactive")
	}
}

func TestGetActiveWorkspaceIDsChatOnly(t *testing.T) {
	m := newTestModel()
	ws1 := newTestWorkspace("ws1", "/repo/ws1")
	ws2 := newTestWorkspace("ws2", "/repo/ws2")

	activeChat := &Tab{
		Assistant: "claude", Workspace: ws1, Running: true,
		HookState: string(hooks.EventPreToolUse),
	}
	viewer := &Tab{
		Assistant: "viewer", Workspace: ws2, Running: true,
		HookState: string(hooks.EventPreToolUse),
	}

	m.tabsByWorkspace[string(ws1.ID())] = []*Tab{activeChat}
	m.tabsByWorkspace[string(ws2.ID())] = []*Tab{viewer}

	ids := m.GetActiveWorkspaceIDs()
	if len(ids) != 1 || ids[0] != string(ws1.ID()) {
		t.Fatalf("expected only ws1 to be active, got %v", ids)
	}
}

func TestIsTabActiveIdle(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	idle := &Tab{
		Assistant: "claude", Workspace: ws, Running: true,
	}
	if m.IsTabActive(idle) {
		t.Fatalf("expected idle chat tab to be inactive")
	}
}

func TestPTYControlTrafficDoesNotActivateTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{ID: "tab-1", Assistant: "claude", Workspace: ws, Running: true}
	m.tabsByWorkspace[string(ws.ID())] = []*Tab{tab}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: string(ws.ID()),
		TabID:       tab.ID,
		Data:        []byte("\x1b[2J\x1b[H\x1b[?25l\x1b[?1049h"),
	})

	if m.IsTabActive(tab) {
		t.Fatal("terminal control traffic marked the tab active")
	}
	if tab.lastOutputAt.IsZero() {
		t.Fatal("control traffic must still update PTY flush timing")
	}
}

func TestPTYVisibleOutputDoesNotActivateTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{ID: "tab-1", Assistant: "claude", Workspace: ws, Running: true}
	m.tabsByWorkspace[string(ws.ID())] = []*Tab{tab}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: string(ws.ID()),
		TabID:       tab.ID,
		Data:        []byte("\x1b[32mworking\x1b[0m"),
	})

	if m.IsTabActive(tab) {
		t.Fatal("PTY output marked the tab active without a lifecycle hook")
	}
}
