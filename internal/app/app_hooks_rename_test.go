package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/common"
)

const (
	launchedAs = "medusa-doablefromhome-bison-plum-1"
	renamedTo  = "medusa-tweaking-dfh-1"
)

// newRenamedTabApp builds an App holding one tab whose workspace was renamed
// while its agent ran: tmux knows the session as renamedTo, while the agent
// still carries MEDUSA_SESSION_NAME=launchedAs and sends it on every hook.
func newRenamedTabApp(t *testing.T, assistant string) (*App, string) {
	t.Helper()
	cfg := &config.Config{Assistants: map[string]config.AssistantConfig{"claude": {}, "codex": {}}}
	root := t.TempDir()
	ws := data.NewWorkspace("tweaking-dfh", "", "", root, root)
	a := &App{
		config:              cfg,
		center:              center.New(cfg),
		toast:               common.NewToastModel(),
		allWorkspaces:       []*data.Workspace{ws},
		dirtyWorkspaces:     map[string]bool{},
		hookWorkspaceStates: map[string]hooks.EventType{},
		hookLastStamp:       map[string]hookEventStamp{},
		hookOutstanding:     map[string]int{},
		hookTabStates:       map[string]hooks.EventType{},
		hookTabLastStamp:    map[string]hookEventStamp{},
		hookTabOutstanding:  map[string]int{},
	}
	a.center.AddTab(&center.Tab{
		Assistant:         assistant,
		Workspace:         ws,
		SessionName:       renamedTo,
		LaunchSessionName: launchedAs,
	})
	return a, string(ws.ID())
}

func tabInfo(t *testing.T, a *App, wsID string) data.TabInfo {
	t.Helper()
	infos, _ := a.center.GetTabsInfoForWorkspace(wsID)
	if len(infos) != 1 {
		t.Fatalf("expected one tab, got %d", len(infos))
	}
	return infos[0]
}

// The regression: a workspace renamed while its agent ran stopped tracking
// activity, because every hook the agent fired named a session the tab no
// longer had. Nothing about it is specific to one assistant.
func TestHookUnderThePreRenameNameReachesTheTab(t *testing.T) {
	for _, assistant := range []string{"claude", "codex"} {
		t.Run(assistant, func(t *testing.T) {
			a, wsID := newRenamedTabApp(t, assistant)

			a.handleHookActivityEvent(hookActivityEvent{
				SessionName: launchedAs, Event: hooks.EventPreToolUse, Timestamp: stamp(1),
				Outstanding: hooks.OutstandingUnknown,
			})
			if got := a.hookWorkspaceStates[wsID]; got != hooks.EventPreToolUse {
				t.Fatalf("workspace state = %q, want %q", got, hooks.EventPreToolUse)
			}
			if got := tabInfo(t, a, wsID).ActivityState; got != string(hooks.EventPreToolUse) {
				t.Fatalf("tab state = %q, want %q", got, hooks.EventPreToolUse)
			}
			// State is keyed by the tab's current name, so the reconciler and
			// the workspace roll-up see one tab, not two.
			if _, ok := a.hookTabStates[launchedAs]; ok {
				t.Error("hook state must not be keyed by the launch name")
			}

			a.handleHookActivityEvent(hookActivityEvent{
				SessionName: launchedAs, Event: hooks.EventStop, Timestamp: stamp(2), Outstanding: 0,
			})
			if _, busy := a.hookWorkspaceStates[wsID]; busy {
				t.Fatal("Stop under the launch name must clear the workspace")
			}
		})
	}
}

// A SessionStart under the tab's current name proves its agent was relaunched
// with that name, so the old one is retired. One under the old name proves
// nothing of the sort: a /clear in the original agent fires it too.
func TestSessionStartUnderTheCurrentNameRetiresTheLaunchName(t *testing.T) {
	a, wsID := newRenamedTabApp(t, "codex")

	a.handleHookActivityEvent(hookActivityEvent{SessionName: launchedAs, Event: hooks.EventSessionStart, Timestamp: stamp(1)})
	if got := tabInfo(t, a, wsID).LaunchSessionName; got != launchedAs {
		t.Fatalf("SessionStart under the launch name retired it (now %q)", got)
	}

	a.handleHookActivityEvent(hookActivityEvent{SessionName: renamedTo, Event: hooks.EventSessionStart, Timestamp: stamp(2)})
	if got := tabInfo(t, a, wsID).LaunchSessionName; got != "" {
		t.Fatalf("LaunchSessionName = %q, want it retired", got)
	}
	if !a.dirtyWorkspaces[wsID] {
		t.Error("retiring the launch name must persist the tab")
	}
}

// A tab busy at the moment of the rename must stay one tab in the hook
// tables, or its old entry is left spinning until the reconciler clears it.
func TestRenameHookSessionsMovesTabState(t *testing.T) {
	a, _ := newRenamedTabApp(t, "claude")
	a.hookTabStates[launchedAs] = hooks.EventPreToolUse
	a.hookTabLastStamp[launchedAs] = hookEventStamp{at: stamp(1)}
	a.hookTabOutstanding[launchedAs] = 2

	a.renameHookSessions(map[string]string{launchedAs: renamedTo})

	if _, ok := a.hookTabStates[launchedAs]; ok {
		t.Error("state left under the old name")
	}
	if a.hookTabStates[renamedTo] != hooks.EventPreToolUse ||
		a.hookTabLastStamp[renamedTo].at != stamp(1) ||
		a.hookTabOutstanding[renamedTo] != 2 {
		t.Errorf("state not moved to the new name: states=%v stamps=%v outstanding=%v",
			a.hookTabStates, a.hookTabLastStamp, a.hookTabOutstanding)
	}
}

// A launch name is only a fallback. A newer tab whose current session name
// happens to equal it owns that name, or its hooks would land on the old tab.
func TestCurrentNameWinsOverAnotherTabsLaunchName(t *testing.T) {
	a, _ := newRenamedTabApp(t, "codex")
	root := t.TempDir()
	reuser := data.NewWorkspace("doablefromhome-bison-plum", "", "", root, root)
	a.allWorkspaces = append(a.allWorkspaces, reuser)
	a.center.AddTab(&center.Tab{Assistant: "claude", Workspace: reuser, SessionName: launchedAs})

	info := a.tabSessionInfoByName()[launchedAs]
	if info.WorkspaceID != string(reuser.ID()) || info.SessionName != launchedAs {
		t.Fatalf("%s resolved to %+v, want the tab that currently has that name", launchedAs, info)
	}
}
