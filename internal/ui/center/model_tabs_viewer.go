package center

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/diff"
)

// createVimTab creates a new tab that opens a file in vim
func (m *Model) createVimTab(filePath string, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating vim viewer"}
		}
	}

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	tabID := generateTabID()

	return func() tea.Msg {
		logging.Info("Creating vim tab: file=%s workspace=%s", filePath, ws.Name)

		sessionName, _ := tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())

		escapedFile := "'" + strings.ReplaceAll(filePath, "'", "'\\''") + "'"
		cmd := fmt.Sprintf("vim -- %s", escapedFile)

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "viewer",
			Assistant:   "viewer",
			CreatedAt:   time.Now().Unix(),
		}
		agent, err := m.agentManager.CreateViewerWithTags(ws, cmd, sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			logging.Error("Failed to create vim viewer: %v", err)
			return messages.Error{Err: err, Context: "creating vim viewer"}
		}

		logging.Info("Vim viewer created, Terminal=%v", agent.Terminal != nil)

		fileName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
			fileName = fileName[idx+1:]
		}
		displayName := truncateDisplayName(fileName)

		return ptyTabCreateResult{
			Workspace:   ws,
			Assistant:   "vim",
			DisplayName: displayName,
			Agent:       agent,
			TabID:       tabID,
			Activate:    true,
			Rows:        termHeight,
			Cols:        termWidth,
		}
	}
}

// createDiffTab creates a new native diff viewer tab (no PTY)
func (m *Model) createDiffTab(change *git.Change, mode git.DiffMode, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating diff viewer"}
		}
	}

	logging.Info("Creating diff tab: path=%s mode=%d workspace=%s", change.Path, mode, ws.Name)

	tm := m.terminalMetrics()
	viewerWidth := tm.Width
	viewerHeight := tm.Height

	dv := diff.New(ws, change, mode, viewerWidth, viewerHeight)
	dv.SetFocused(true)

	wsID := string(ws.ID())
	displayName := fmt.Sprintf("Diff: %s", change.Path)
	if len(displayName) > 20 {
		displayName = "..." + displayName[len(displayName)-17:]
	}

	tab := &Tab{
		ID:         generateTabID(),
		Name:       displayName,
		Assistant:  "diff",
		Workspace:  ws,
		DiffViewer: dv,
	}

	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
	m.activeTabByWorkspace[wsID] = len(m.tabsByWorkspace[wsID]) - 1
	m.noteTabsChanged()

	return common.SafeBatch(
		dv.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.activeTabByWorkspace[wsID], Name: displayName} },
	)
}
