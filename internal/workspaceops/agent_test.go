package workspaceops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/tmux"
)

// agentDeps is newDeps with a private tmux server and a stand-in for the
// assistant, so no real agent starts.
func agentDeps(t *testing.T) Deps {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	server := fmt.Sprintf("medusa-ops-test-%d", os.Getpid())
	t.Setenv("MEDUSA_TMUX_SERVER", server)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", server, "kill-server").Run() })
	d := newDeps(t)
	d.Config.Assistants = map[string]config.AssistantConfig{
		AssistantClaude: {Command: "sleep 30"},
		AssistantCodex:  {Command: "sleep 30"},
	}
	return d
}

func TestStartAgentRecordsATabTheTUICanAdopt(t *testing.T) {
	d := agentDeps(t)
	repo := newRepo(t)
	ws, err := d.Create(CreateRequest{Name: "agentic", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Work", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}

	started, err := d.StartAgent(ws, Launch{Assistant: AssistantClaude, PermissionMode: "plan", Fullscreen: true}, "secret first prompt")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	opts := tmux.DefaultOptions()
	if got, _ := tmux.SessionTagValue(started.SessionName, "@medusa_workspace", opts); got != string(ws.ID()) {
		t.Fatalf("@medusa_workspace = %q, want %q", got, ws.ID())
	}
	if got, _ := tmux.SessionTagValue(started.SessionName, "@medusa_type", opts); got != "agent" {
		t.Fatalf("@medusa_type = %q, want agent", got)
	}

	stored, err := d.Store.Load(ws.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.OpenTabs) != 1 {
		t.Fatalf("stored %d tabs, want 1", len(stored.OpenTabs))
	}
	tab := stored.OpenTabs[0]
	if tab.Assistant != AssistantClaude || tab.Name != AssistantClaude || tab.SessionName != started.SessionName ||
		tab.Status != "running" || tab.PermissionMode != "plan" || !tab.Fullscreen {
		t.Fatalf("stored tab = %+v", tab)
	}
	if tab.ClaudeSessionID == "" || tab.ClaudeSessionID != started.SessionID {
		t.Fatalf("ClaudeSessionID = %q, started.SessionID = %q; want the same non-empty id", tab.ClaudeSessionID, started.SessionID)
	}
	raw, _ := os.ReadFile(d.Config.Paths.MetadataRoot + "/" + string(ws.ID()) + "/workspace.json")
	if strings.Contains(string(raw), "secret first prompt") {
		t.Fatal("the first prompt was persisted, so a restore could replay it")
	}
}

func TestStartAgentCodexHasNoPreassignedSession(t *testing.T) {
	d := agentDeps(t)
	ws, err := d.Create(CreateRequest{Name: "cx", Repos: []data.RepoRef{newRepo(t)}, Bases: []string{"main"}, Profile: "Work", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	started, err := d.StartAgent(ws, Launch{Assistant: AssistantCodex, CodexSandbox: "read-only"}, "")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if started.SessionID != "" || started.Tab.CodexSandbox != "read-only" || started.Tab.Fullscreen {
		t.Fatalf("codex start = %+v", started)
	}
}
