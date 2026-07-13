package vterm

// maxLinkTable bounds the interning table. A pane that emits an unbounded
// stream of distinct URIs (a log tailing unique links, say) would otherwise
// grow the table forever, since scrollback can still reference old IDs. Past
// the cap new URIs render as plain text rather than evicting live IDs.
const maxLinkTable = 4096

// Link is an OSC 8 hyperlink target. Params carries the sequence's parameter
// list (tmux sets id=, which lets a terminal treat a link that wraps across
// lines as a single link).
type Link struct {
	URI    string
	Params string
}

// internLink returns a stable ID for a hyperlink target, reusing the ID of an
// identical target. An empty URI means "no hyperlink" and always maps to 0.
func (v *VTerm) internLink(uri, params string) uint32 {
	if uri == "" {
		return 0
	}
	key := params + "\x00" + uri
	if id, ok := v.linkIDs[key]; ok {
		return id
	}
	if len(v.links) >= maxLinkTable {
		return 0
	}
	if v.linkIDs == nil {
		v.linkIDs = make(map[string]uint32)
	}
	v.links = append(v.links, Link{URI: uri, Params: params})
	id := uint32(len(v.links)) // IDs are 1-based so that 0 means "no link"
	v.linkIDs[key] = id
	return id
}

// LinkTarget resolves an interned hyperlink ID. It returns empty strings for
// ID 0 or an ID this terminal never issued.
func (v *VTerm) LinkTarget(id uint32) (uri, params string) {
	if id == 0 || int(id) > len(v.links) {
		return "", ""
	}
	link := v.links[id-1]
	return link.URI, link.Params
}

// LinkTableLen reports how many distinct hyperlink targets are interned.
func (v *VTerm) LinkTableLen() int {
	return len(v.links)
}

// LinkTable returns a copy of the hyperlink table, indexable by ID-1. Callers
// rendering a snapshot use it to resolve Cell.Link without holding the lock.
func (v *VTerm) LinkTable() []Link {
	if len(v.links) == 0 {
		return nil
	}
	out := make([]Link, len(v.links))
	copy(out, v.links)
	return out
}

// setHyperlink starts or ends the hyperlink applied to newly written cells.
// An empty URI ends the current link, which is how OSC 8 ;; ST closes one.
func (v *VTerm) setHyperlink(uri, params string) {
	v.CurrentLink = v.internLink(uri, params)
}
