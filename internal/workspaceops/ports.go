package workspaceops

import (
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/process"
)

// SyncPorts seeds d.Ports with every workspace's persisted port, then
// allocates and persists a port for each workspace that has none (records from
// before ports were persisted, or a persisted port that collided with an
// earlier one). The workspaces must be freshly read from disk and not yet
// shared with another goroutine, so saving them cannot clobber newer state.
func (d Deps) SyncPorts(workspaces []*data.Workspace) {
	if d.Ports == nil {
		return
	}
	rejected := make(map[string]bool)
	for _, key := range d.Ports.Seed(portSeeds(workspaces)) {
		rejected[key] = true
	}
	for _, ws := range workspaces {
		if ws.IsOrphaned() || (ws.Port > 0 && !rejected[string(ws.ID())]) {
			continue
		}
		d.persistPort(ws, d.Ports.AllocatePort(string(ws.ID())))
	}
}

// seedPortsFromDisk loads every registered workspace's persisted port into
// d.Ports, so the next allocation skips ports another medusa process (the CLI,
// or a TUI that has not reloaded yet) handed out. It persists nothing.
func (d Deps) seedPortsFromDisk() {
	entries, err := d.Registry.ListWorkspaces()
	if err != nil {
		logging.Warn("Could not read the registry to seed ports: %v", err)
		return
	}
	var workspaces []*data.Workspace
	for _, entry := range entries {
		if ws, err := d.Store.Load(data.WorkspaceID(entry.ID)); err == nil && ws != nil {
			workspaces = append(workspaces, ws)
		}
	}
	d.Ports.Seed(portSeeds(workspaces))
}

func portSeeds(workspaces []*data.Workspace) []process.PortSeed {
	var seeds []process.PortSeed
	for _, ws := range workspaces {
		if ws.Orphan != data.OrphanDirectory && ws.Port > 0 {
			seeds = append(seeds, process.PortSeed{Key: string(ws.ID()), Port: ws.Port})
		}
	}
	return seeds
}

func (d Deps) persistPort(ws *data.Workspace, port int) {
	previous := ws.Port
	ws.Port = port
	if err := d.Store.Save(ws); err != nil {
		logging.Warn("Failed to persist port %d for workspace %s (was %d): %v", port, ws.Name, previous, err)
		return
	}
	logging.Info("Assigned port %d to workspace %s (was %d)", port, ws.Name, previous)
}

// assignPort gives a fresh workspace its port before its first save, so the
// record carries it from the start.
func (d Deps) assignPort(ws *data.Workspace) {
	if d.Ports == nil {
		return
	}
	d.seedPortsFromDisk()
	ws.Port = d.Ports.AllocatePort(string(ws.ID()))
}

// releasePort frees a deleted workspace's port range.
func (d Deps) releasePort(ws *data.Workspace) {
	if d.Ports != nil {
		d.Ports.ReleasePort(string(ws.ID()))
	}
}
