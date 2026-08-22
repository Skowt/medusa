package app

// Shutdown releases resources that may outlive the Bubble Tea program.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(func() {
		// Close terminals and scripts first. The supervisor's tab actor
		// may be blocked on a PTY write (a raw syscall that ignores
		// context cancellation). Closing the PTY file descriptors here
		// unblocks those writes so the supervisor can stop cleanly.
		if a.center != nil {
			a.center.Close()
		}
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.CloseAll()
		}
		if a.scripts != nil {
			a.scripts.StopAll()
		}
		if a.supervisor != nil {
			a.supervisor.Stop()
		}
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Close()
		}
		if a.hooksServer != nil {
			_ = a.hooksServer.Close()
		}
		if a.skillUsage != nil {
			_ = a.skillUsage.Close()
		}
	})
}
