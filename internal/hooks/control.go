package hooks

import (
	"encoding/json"
	"net"
	"time"
)

// Control events are not Claude Code hooks: `medusa workspace` sends them to a
// running TUI over the same socket. They name no session, which the server
// otherwise requires of every event.
const (
	// EventWorkspacesChanged asks the TUI to reload workspaces from disk.
	EventWorkspacesChanged EventType = "medusa:workspaces-changed"
	// EventWorkspaceClosing asks the TUI to drop a workspace's tabs before
	// its tmux sessions are killed, so it does not auto-reattach to them.
	EventWorkspaceClosing EventType = "medusa:workspace-closing"
)

// notifyDialTimeout bounds how long a CLI command waits on a socket nobody is
// listening on: a stale socket file must not stall a script.
const notifyDialTimeout = 500 * time.Millisecond

// IsControlEvent reports whether e is sent by medusa itself rather than by an
// agent's hook.
func IsControlEvent(e EventType) bool {
	return e == EventWorkspacesChanged || e == EventWorkspaceClosing
}

// NotifyWorkspacesChanged tells a running TUI to reload workspaces. It
// returns false when no TUI is listening.
func NotifyWorkspacesChanged(hooksDir string) bool {
	return notify(hooksDir, map[string]string{"event": string(EventWorkspacesChanged)})
}

// NotifyWorkspaceClosing tells a running TUI that a workspace is about to be
// deleted. It returns false when no TUI is listening.
func NotifyWorkspaceClosing(hooksDir, workspaceID string) bool {
	return notify(hooksDir, map[string]string{"event": string(EventWorkspaceClosing), "workspace": workspaceID})
}

func notify(hooksDir string, payload map[string]string) bool {
	line, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", SocketPath(hooksDir), notifyDialTimeout)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(notifyDialTimeout))
	_, err = conn.Write(append(line, '\n'))
	return err == nil
}
