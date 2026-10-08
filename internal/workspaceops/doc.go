// Package workspaceops creates and deletes workspaces and resolves how their
// agents launch. It is the one implementation behind both the TUI and the
// headless `medusa workspace` commands, so the two cannot disagree about what
// a workspace is or what deleting one removes. It must not import internal/app
// or internal/ui.
package workspaceops

import (
	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
)

// Deps are the stores an operation reads and writes.
type Deps struct {
	Config   *config.Config
	Registry *data.Registry
	Store    *data.WorkspaceStore
	Recents  *data.RecentsStore
}
