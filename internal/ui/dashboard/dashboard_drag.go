package dashboard

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// dragKind identifies what a drag carries. It doubles as the kind of thing the
// pointer is hovering, which is the same two-way choice.
type dragKind int

const (
	dragNone dragKind = iota
	dragWorkspace
	dragGroup
)

// dragPromoteLines is how far the pointer must travel vertically before a press
// becomes a drag. A workspace row is two to four lines tall, so a single line of
// slop would promote on the jitter of an ordinary click and swallow it.
const dragPromoteLines = 2

// dragState tracks an in-progress row drag.
//
// Identities are stored as roots and group keys, never row indices:
// rebuildRows runs on every workspace update, hook event and spinner tick, and
// an index would silently come to mean whatever row had moved into its place.
type dragState struct {
	kind   dragKind
	active bool // promoted past dragPromoteLines; below it this is still a click
	startY int

	srcRoot  string // dragWorkspace: the workspace being moved
	srcGroup string // dragGroup: the section being moved

	// Projected placement. rebuildRows renders the drag as if it had already
	// landed here, so what the user sees during the drag is the order the
	// release commits — there is no separate "drop target" to reconcile with it.
	// placed is false until the pointer first reaches a valid slot.
	placed        bool
	placeGroup    string // dragWorkspace: the group it is projected into
	placeIndex    int    // index within that group's members, or among the section keys
	placeNewGroup bool   // dragWorkspace: projected into a group the release will create
}

// beginDragCandidate records a press on a draggable row without committing to a
// drag: whether the press was a click or the start of a drag is only known once
// the pointer moves, so the row's own action waits for the release. Reports
// false for rows that cannot be dragged, which activate on press as before.
func (m *Model) beginDragCandidate(idx, screenY int) bool {
	m.drag = dragState{}
	if idx < 0 || idx >= len(m.rows) {
		return false
	}
	row := m.rows[idx]
	switch {
	case row.Type == RowWorkspace && m.draggableWorkspace(row.Workspace):
		m.drag = dragState{kind: dragWorkspace, startY: screenY, srcRoot: row.Workspace.Root()}
		return true
	case row.Type == RowSectionHeader && m.draggableGroup(row):
		m.drag = dragState{kind: dragGroup, startY: screenY, srcGroup: labelToKey(row.Label)}
		return true
	}
	return false
}

// draggableWorkspace reports whether a workspace row can be dragged or dropped
// onto. A workspace mid-create is excluded on top of the archived and orphaned
// rows: it has no registry entry yet, so there is nothing to persist an order
// against.
func (m *Model) draggableWorkspace(ws *data.Workspace) bool {
	return orderable(ws) && !m.workspacePending(ws)
}

// draggableGroup reports whether a section header can be reordered. Ungrouped
// cannot: it is pinned to the bottom, so there is no position to give it.
func (m *Model) draggableGroup(row Row) bool {
	return row.Type == RowSectionHeader && row.IsUserGroup && labelToKey(row.Label) != ""
}

// updateDragMotion advances an in-progress drag, reporting whether the drag
// consumed the event.
func (m *Model) updateDragMotion(msg tea.MouseMotionMsg) bool {
	if m.drag.kind == dragNone {
		return false
	}
	if msg.Button != tea.MouseLeft {
		// Motion without the button held means the release went somewhere we
		// never saw. Abandon the candidate rather than leave it armed.
		m.drag = dragState{}
		return false
	}
	if !m.drag.active {
		if abs(msg.Y-m.drag.startY) < dragPromoteLines {
			return true
		}
		m.drag.active = true
		m.clearHover()
		// Settle the rows before resolving the pointer against them. Promotion
		// inserts the New group target, which shifts everything below it, and
		// resolving against the pre-promotion layout would land the drop a row
		// or two from where the user is looking.
		m.rebuildRows()
	}
	m.dragAutoScroll(msg.Y)
	if m.updateProjection(msg.X, msg.Y) {
		m.rebuildRows()
	}
	return true
}

// showNewGroupRow reports whether the "New group" drop target belongs in the
// rows. It is a target, not a button: nothing outside a workspace drag can use
// it, so nothing outside a workspace drag shows it.
func (m *Model) showNewGroupRow() bool {
	return m.drag.active && m.drag.kind == dragWorkspace
}

// updateProjection moves the projected placement to follow the pointer,
// reporting whether it changed. A pointer over nothing droppable leaves the
// current projection alone rather than snapping the row back, so passing over a
// spacer or the archived drawer on the way somewhere does not undo the drag in
// progress.
func (m *Model) updateProjection(screenX, screenY int) bool {
	switch m.drag.kind {
	case dragWorkspace:
		idx, _, ok := m.rowIndexAt(screenX, screenY)
		if !ok {
			return false
		}
		slot, ok := m.displayedWorkspaceSlot(idx)
		if !ok {
			return false
		}
		if m.drag.placed && m.drag.placeGroup == slot.group &&
			m.drag.placeIndex == slot.index && m.drag.placeNewGroup == slot.newGroup {
			return false
		}
		m.drag.placed = true
		m.drag.placeGroup = slot.group
		m.drag.placeIndex = slot.index
		m.drag.placeNewGroup = slot.newGroup
		return true
	case dragGroup:
		contentY, _, ok := m.rowAreaHit(screenX, screenY)
		if !ok {
			return false
		}
		return m.updateGroupProjection(contentY + m.scrollOffset)
	}
	return false
}

// workspaceSlot is where a dropped workspace would land: a position in an
// existing group, or a group the release has to create.
type workspaceSlot struct {
	group    string
	index    int
	newGroup bool
}

// displayedWorkspaceSlot maps a row to the slot a workspace dropped there would
// occupy. The index counts only orderable member rows, so it lines up with the
// list the commit is built from even while a sibling workspace is mid-create and
// rendering a placeholder row of its own.
func (m *Model) displayedWorkspaceSlot(idx int) (workspaceSlot, bool) {
	if idx < 0 || idx >= len(m.rows) {
		return workspaceSlot{}, false
	}
	row := m.rows[idx]
	switch {
	case row.Type == RowNewGroup:
		return workspaceSlot{newGroup: true}, true
	// A header means the top of its section — the only way into a collapsed
	// group, which renders no member rows to aim at.
	case row.Type == RowSectionHeader && row.IsUserGroup:
		return workspaceSlot{group: labelToKey(row.Label)}, true
	case row.Type != RowWorkspace || !m.draggableWorkspace(row.Workspace):
		return workspaceSlot{}, false
	}

	pos := 0
	for i := idx - 1; i >= 0; i-- {
		prev := m.rows[i]
		switch {
		case prev.Type == RowNewGroup:
			return workspaceSlot{newGroup: true, index: pos}, true
		case prev.Type == RowSectionHeader:
			if !prev.IsUserGroup {
				return workspaceSlot{}, false // archived / orphans drawer
			}
			return workspaceSlot{group: labelToKey(prev.Label), index: pos}, true
		case prev.Type == RowWorkspace && m.draggableWorkspace(prev.Workspace):
			pos++
		}
	}
	return workspaceSlot{}, false
}

// sectionExtent is one section's rendered line span, headers and members and the
// trailing spacer together, in the row area's own line coordinates.
type sectionExtent struct {
	key        string
	start, end int
}

func (e sectionExtent) height() int { return e.end - e.start }

// sectionExtents measures the reorderable sections as they are currently
// rendered. It stops at the orphans or archived drawer and at the create button,
// which are not part of the reorderable region.
func (m *Model) sectionExtents() []sectionExtent {
	var exts []sectionExtent
	line := 0
	for i, row := range m.rows {
		lines := m.rowLineCount(i)
		switch {
		case row.Type == RowSectionHeader && row.IsUserGroup:
			exts = append(exts, sectionExtent{key: labelToKey(row.Label), start: line, end: line + lines})
		case row.Type == RowSectionHeader || row.Type == RowCreate:
			return exts
		case len(exts) > 0 && (row.Type == RowWorkspace || row.Type == RowSpacer):
			exts[len(exts)-1].end = line + lines
		}
		line += lines
	}
	return exts
}

// updateGroupProjection walks a dragged section one place at a time, and only
// once the pointer has passed the middle of the neighbour it would displace.
//
// Resolving "which section is the pointer over" instead — the way a workspace
// drag does — jitters badly for sections, because they are tall and unequal: the
// dragged section lands somewhere the pointer is no longer inside, the next event
// resolves to the section that took its place, and it flips between the two
// forever. Sections are not rows and cannot borrow the rows' fixed point.
//
// Half of the displaced neighbour is the threshold that makes the two directions
// disjoint. Displacing a neighbour of height e downward needs the pointer at
// least e/2 past this section's end; afterwards, moving back up would need it
// more than e/2 above the section's new start, and those two bands cannot both
// hold. So each event moves the section at most one place, always toward the
// pointer, and holding still holds still.
func (m *Model) updateGroupProjection(line int) bool {
	exts := m.sectionExtents()
	at := -1
	for i, ext := range exts {
		if ext.key == m.drag.srcGroup {
			at = i
			break
		}
	}
	if at < 0 {
		return false
	}
	current := exts[at]

	if line >= current.end && at+1 < len(exts) {
		next := exts[at+1]
		// Ungrouped is pinned to the bottom; nothing displaces it.
		if next.key != "" && line >= current.end+next.height()/2 {
			return m.setGroupIndex(at + 1)
		}
	}
	if line < current.start && at > 0 {
		prev := exts[at-1]
		if line < current.start-prev.height()/2 {
			return m.setGroupIndex(at - 1)
		}
	}
	return false
}

// setGroupIndex records a new projected section index, reporting whether it
// changed. The index is read against the displayed order, which holds the same
// keys as the stored order, so it means the same position in both.
func (m *Model) setGroupIndex(idx int) bool {
	if m.drag.placed && m.drag.placeIndex == idx {
		return false
	}
	m.drag.placed = true
	m.drag.placeIndex = idx
	return true
}

// displayedSectionKeys returns the section keys in the order they are rendered,
// which during a drag is the projected order.
func (m *Model) displayedSectionKeys() []string {
	var keys []string
	for _, row := range m.rows {
		if row.Type == RowSectionHeader && row.IsUserGroup {
			keys = append(keys, labelToKey(row.Label))
		}
	}
	return keys
}

// finishDrag ends whatever the last press started and clears the drag. It
// returns the reorder command for a completed drop, or — when the pointer never
// travelled far enough to promote the press into a drag — the row action the
// press deferred.
func (m *Model) finishDrag() tea.Cmd {
	d := m.drag
	m.drag = dragState{}

	switch {
	case d.kind == dragNone:
		return nil
	case !d.active:
		return m.deferredClick(d)
	case !d.placed:
		// Promoted but never over a valid slot: nothing moved, so nothing to
		// commit. rebuildRows has to run anyway to drop the lifted styling.
		m.rebuildRows()
		return nil
	case d.kind == dragWorkspace:
		return m.commitWorkspaceDrop(d)
	case d.kind == dragGroup:
		return m.commitGroupDrop(d)
	}
	return nil
}

// cancelDrag abandons an in-progress drag, restoring the untouched order.
// Reports whether there was one to cancel.
func (m *Model) cancelDrag() bool {
	if m.drag.kind == dragNone {
		return false
	}
	m.drag = dragState{}
	m.rebuildRows()
	return true
}

// deferredClick runs the action the press held back. The cursor is re-anchored
// by identity first: a rebuild between press and release can have moved the row
// the cursor was parked on.
func (m *Model) deferredClick(d dragState) tea.Cmd {
	for i, row := range m.rows {
		switch {
		case d.kind == dragWorkspace && row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root() == d.srcRoot:
			m.cursor = i
			return m.handleClick()
		case d.kind == dragGroup && row.Type == RowSectionHeader && row.IsUserGroup && labelToKey(row.Label) == d.srcGroup:
			m.cursor = i
			return m.handleToggleCollapse()
		}
	}
	return nil
}

// commitWorkspaceDrop turns the projection into a reorder message.
func (m *Model) commitWorkspaceDrop(d dragState) tea.Cmd {
	src := m.workspaceByRoot(d.srcRoot)
	if src == nil {
		m.rebuildRows()
		return nil
	}
	if d.placeNewGroup {
		// Naming happens here because a group is only ever the label its members
		// share: there is nothing to create but the label.
		group, root := newGroupName(m.groupLabels()), d.srcRoot
		// The group is also pinned last, where it was dropped. Without this it
		// would fall back to the alphabetical order every undragged group uses,
		// and a generated name starting with an "a" would leap to the top of the
		// pane the instant it was created at the bottom.
		order := append(without(m.currentSectionKeys(), ""), group)
		m.rebuildRows()
		return func() tea.Msg {
			return messages.CreateGroupForWorkspace{Root: root, Label: group, Order: order}
		}
	}
	ordered := m.projectedGroupRoots(d.placeGroup, d.srcRoot, d.placeIndex)
	if d.placeGroup == src.Group && sameOrder(ordered, m.orderedGroupMembers(d.placeGroup)) {
		m.rebuildRows()
		return nil
	}
	group := d.placeGroup
	return func() tea.Msg {
		return messages.ReorderWorkspaces{Group: group, OrderedRoots: ordered}
	}
}

// commitGroupDrop turns the projection into a group reorder message. Ungrouped
// is left out of what is persisted: it is pinned to the bottom, so a stored
// position for it would never be read.
func (m *Model) commitGroupDrop(d dragState) tea.Cmd {
	keys := m.currentSectionKeys()
	if indexOf(keys, d.srcGroup) < 0 {
		m.rebuildRows()
		return nil
	}
	ordered := without(moveToIndex(keys, d.srcGroup, d.placeIndex), "")
	if sameOrder(ordered, without(keys, "")) {
		m.rebuildRows()
		return nil
	}
	return func() tea.Msg {
		return messages.ReorderGroups{Labels: ordered}
	}
}

// dragAutoScroll nudges the row area when a drag reaches its top or bottom
// edge, so a workspace can be carried past the visible window.
func (m *Model) dragAutoScroll(screenY int) {
	height, ok := m.rowAreaHeight()
	if !ok {
		return
	}
	contentY := screenY - 1 // top border
	switch {
	case contentY <= 0:
		m.scrollOffset--
	case contentY >= height-1:
		m.scrollOffset++
	}
	m.clampScrollOffset()
}

// isDragSourceWorkspace reports whether a workspace row is the one being
// carried, so the renderer can show it as lifted.
func (m *Model) isDragSourceWorkspace(ws *data.Workspace) bool {
	return m.drag.active && m.drag.kind == dragWorkspace && ws != nil && ws.Root() == m.drag.srcRoot
}

// isDragSourceGroup reports whether a group section is the one being carried.
// Its members are withheld while it is: a whole section jumping about as the
// pointer moves says less about where it will land than one placeholder row
// does, and it keeps the rows under the pointer from shifting by more than a
// line at a time.
func (m *Model) isDragSourceGroup(key string) bool {
	return m.drag.active && m.drag.kind == dragGroup && key == m.drag.srcGroup
}

// hoverState tracks the row under an unpressed pointer, so the renderer can show
// a drag handle on exactly the rows that have one.
type hoverState struct {
	kind  dragKind
	root  string
	group string
}

// updateHover records what an unpressed pointer is over. Hover is tracked
// whether or not the pane holds focus: the handle exists to advertise that a row
// can be dragged, and a press is what takes focus in the first place.
func (m *Model) updateHover(screenX, screenY int) {
	if m.drag.kind != dragNone {
		m.clearHover()
		return
	}
	next := hoverState{}
	if idx, _, ok := m.rowIndexAt(screenX, screenY); ok && idx >= 0 && idx < len(m.rows) {
		row := m.rows[idx]
		switch {
		case row.Type == RowWorkspace && m.draggableWorkspace(row.Workspace):
			next = hoverState{kind: dragWorkspace, root: row.Workspace.Root()}
		case m.draggableGroup(row):
			next = hoverState{kind: dragGroup, group: labelToKey(row.Label)}
		}
	}
	m.hover = next
}

func (m *Model) clearHover() {
	m.hover = hoverState{}
}

// isHoveredWorkspace reports whether a workspace row should show its handle.
func (m *Model) isHoveredWorkspace(ws *data.Workspace) bool {
	return m.hover.kind == dragWorkspace && ws != nil && ws.Root() == m.hover.root
}

// isHoveredGroup reports whether a group header should show its handle.
func (m *Model) isHoveredGroup(key string) bool {
	return m.hover.kind == dragGroup && key == m.hover.group
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
