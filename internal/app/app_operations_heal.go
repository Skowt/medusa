package app

import (
	"os"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
)

// registryHealer reunites a registry entry with metadata that has moved out
// from under it.
//
// Nothing guarantees an entry's ID still addresses its workspace.
// WorkspaceStore.Save rehomes a workspace whenever ws.ID() disagrees with the
// directory it was loaded from, and deletes the directory it moved off —
// without telling the registry. Current medusa never changes an ID it has
// already minted, so it cannot strand an entry itself, but a build predating
// data.Workspace.StableID recomputes the old repo-plus-root hash instead of
// reading the stored one. Running one of those against a shared ~/.medusa
// (which self-hosting on medusa makes easy: any worktree holding an older
// commit builds one) rehomes the metadata and leaves the entry pointing at a
// directory that build has just removed.
//
// The workspace then disappears twice over: loadWorkspaces skips it, and the
// worktree it owns resurfaces in the dashboard as an OrphanDirectory — "no
// metadata" — whose only offered action is deleting the user's work. Repointing
// the entry is what keeps a store nobody corrupted from reading as one they did.
type registryHealer struct {
	store   *data.WorkspaceStore
	claimed map[data.WorkspaceID]bool
	byName  map[string][]healCandidate
}

type healCandidate struct {
	id data.WorkspaceID
	ws *data.Workspace
}

// newRegistryHealer builds a healer that will not adopt any of claimed, the
// store IDs the registry already accounts for.
func newRegistryHealer(store *data.WorkspaceStore, claimed map[data.WorkspaceID]bool) *registryHealer {
	return &registryHealer{store: store, claimed: claimed}
}

// healStrandedEntry reclaims the metadata for a registry entry whose ID no
// longer resolves and repoints the registry at what it finds, returning a nil
// workspace when nothing certain enough turned up.
func (a *App) healStrandedEntry(healer *registryHealer, name, staleID string) (*data.Workspace, data.WorkspaceID) {
	healedID, healed := healer.find(name)
	if healed == nil {
		return nil, ""
	}
	logging.Info("Registry heal: workspace %s is stored under %s, not %s; repointing the registry",
		name, healedID, staleID)
	if err := a.registry.UpdateWorkspace(staleID, name, string(healedID)); err != nil {
		// Surface the workspace regardless. The registry is no worse off than
		// it already was, and the next start retries the repoint.
		logging.Warn("Registry heal: failed to repoint %s: %v", name, err)
	}
	return healed, healedID
}

// find returns the store entry that actually holds name's workspace, or a nil
// workspace when nothing is certain enough to adopt.
//
// Name is the only key available — the registry records nothing else that
// survives an ID change — and it is a sound one, since two live workspaces may
// not share a name. It is not sufficient on its own, though: the store keeps
// every directory the registry has ever dropped, so a name can easily be shared
// with a workspace that died long ago. A candidate is therefore adopted only
// when it is the single unclaimed one left standing. Guessing wrong would
// attach the entry to some other workspace's worktree, which is worse than
// leaving it stranded.
func (h *registryHealer) find(name string) (data.WorkspaceID, *data.Workspace) {
	h.scan()

	var candidates []healCandidate
	for _, c := range h.byName[name] {
		if !h.claimed[c.id] {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) > 1 {
		candidates = withExistingRoot(candidates)
	}
	if len(candidates) != 1 {
		if len(candidates) > 1 {
			logging.Warn("Registry heal: %q matches %d unclaimed store entries, leaving it alone", name, len(candidates))
		}
		return "", nil
	}

	// Claim it, so two entries sharing a name cannot both adopt it.
	chosen := candidates[0]
	h.claimed[chosen.id] = true
	return chosen.id, chosen.ws
}

// scan indexes the store by workspace name, once and only if a heal is actually
// needed. Loading every workspace is not free and the overwhelmingly common
// case is a registry with nothing to repair.
func (h *registryHealer) scan() {
	if h.byName != nil {
		return
	}
	h.byName = make(map[string][]healCandidate)

	ids, err := h.store.List()
	if err != nil {
		logging.Warn("Registry heal: cannot list the workspace store: %v", err)
		return
	}
	for _, id := range ids {
		if h.claimed[id] {
			continue
		}
		ws, err := h.store.Load(id)
		if err != nil || ws == nil {
			continue
		}
		h.byName[ws.Name] = append(h.byName[ws.Name], healCandidate{id: id, ws: ws})
	}
}

// withExistingRoot narrows an ambiguous name match to the candidates whose
// worktree is still on disk, which is what separates a live workspace from the
// stale metadata of a dead one that happened to share its name. It gives up and
// returns the original set when that leaves nothing, so the caller still sees
// the ambiguity rather than an empty result it would read as "no match".
func withExistingRoot(candidates []healCandidate) []healCandidate {
	var live []healCandidate
	for _, c := range candidates {
		root := c.ws.PrimaryWorktreeRoot()
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); err == nil {
			live = append(live, c)
		}
	}
	if len(live) == 0 {
		return candidates
	}
	return live
}
