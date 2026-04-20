package app

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/update"
)

func (a *App) checkForUpdates() tea.Cmd {
	return func() tea.Msg {
		updater := update.NewUpdater(a.version, a.commit, a.buildDate)
		result, err := updater.Check()
		if err != nil {
			logging.Warn("Update check failed: %v", err)
			return messages.UpdateCheckComplete{Err: err}
		}
		return messages.UpdateCheckComplete{
			CurrentVersion:  result.CurrentVersion,
			LatestVersion:   result.LatestVersion,
			UpdateAvailable: result.UpdateAvailable,
			ReleaseNotes:    result.ReleaseNotes,
			Err:             nil,
		}
	}
}

// tmuxAvailableResult is sent after checking tmux availability
type tmuxAvailableResult struct {
	available   bool
	installHint string
}

func (a *App) checkTmuxAvailable() tea.Cmd {
	return func() tea.Msg {
		if err := tmux.EnsureAvailable(); err != nil {
			return tmuxAvailableResult{available: false, installHint: tmux.InstallHint()}
		}
		return tmuxAvailableResult{available: true}
	}
}

// IsTmuxAvailable returns whether tmux is installed and available.
func (a *App) IsTmuxAvailable() bool {
	return a.tmuxAvailable
}

// startGitStatusTicker returns a command that ticks every 3 seconds for git status refresh
func (a *App) startGitStatusTicker() tea.Cmd {
	return common.SafeTick(3*time.Second, func(t time.Time) tea.Msg {
		return messages.GitStatusTick{}
	})
}

// startPTYWatchdog ticks periodically to ensure PTY readers are running.
func (a *App) startPTYWatchdog() tea.Cmd {
	return common.SafeTick(5*time.Second, func(time.Time) tea.Msg {
		return messages.PTYWatchdogTick{}
	})
}

// startTmuxSyncTicker returns a command that ticks for tmux session reconciliation.
func (a *App) startTmuxSyncTicker() tea.Cmd {
	a.tmuxSyncToken++
	token := a.tmuxSyncToken
	return common.SafeTick(a.tmuxSyncInterval(), func(time.Time) tea.Msg {
		return messages.TmuxSyncTick{Token: token}
	})
}

func (a *App) tmuxSyncInterval() time.Duration {
	const defaultInterval = 7 * time.Second
	value := strings.TrimSpace(os.Getenv("MEDUSA_TMUX_SYNC_INTERVAL"))
	if value == "" {
		return defaultInterval
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		logging.Warn("Invalid MEDUSA_TMUX_SYNC_INTERVAL=%q; using %s", value, defaultInterval)
		return defaultInterval
	}
	return interval
}

func applyTmuxEnvFromConfig(cfg *config.Config, force bool) {
	if cfg == nil {
		return
	}
	if force {
		setEnvOrUnset("MEDUSA_TMUX_SERVER", cfg.UI.TmuxServer)
		setEnvOrUnset("MEDUSA_TMUX_CONFIG", cfg.UI.TmuxConfigPath)
		setEnvOrUnset("MEDUSA_TMUX_SYNC_INTERVAL", cfg.UI.TmuxSyncInterval)
		return
	}
	setEnvIfNonEmpty("MEDUSA_TMUX_SERVER", cfg.UI.TmuxServer)
	setEnvIfNonEmpty("MEDUSA_TMUX_CONFIG", cfg.UI.TmuxConfigPath)
	setEnvIfNonEmpty("MEDUSA_TMUX_SYNC_INTERVAL", cfg.UI.TmuxSyncInterval)
}

func (a *App) tmuxSyncWorkspaces() []*data.Workspace {
	if a.monitorMode {
		var targets []*data.Workspace
		for _, ws := range a.allWorkspaces {
			if a.monitorFilter != "" && ws.Root() != a.monitorFilter {
				continue
			}
			targets = append(targets, ws)
		}
		return targets
	}
	if a.activeWorkspace != nil {
		return []*data.Workspace{a.activeWorkspace}
	}
	return nil
}

// startFileWatcher starts watching for file changes and returns events
func (a *App) startFileWatcher() tea.Cmd {
	if a.fileWatcher == nil || a.fileWatcherCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.fileWatcherCh
	}
}
