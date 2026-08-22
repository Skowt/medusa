package center

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// renderTabBar renders the tab bar: pinned Info tab, a horizontally
// scrollable strip of agent tabs, a pinned "+ New Agent" button, and the
// workspace note right-aligned at the content edge.
func (m *Model) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if m.infoTabActive {
		activeIdx = -1
	}
	width := m.contentWidth()

	// --- Zone 1: pinned Info tab ---
	infoRendered := m.renderInfoTab()
	infoWidth := lipgloss.Width(infoRendered)

	// --- Zone 3: pinned "+ New Agent" (omitted for archived workspaces) ---
	var plusRendered string
	if m.workspace == nil || !m.workspace.Archived() {
		plusRendered = m.styles.TabPlus.Render("+ New Agent")
	}
	plusWidth := lipgloss.Width(plusRendered)

	// Measure each agent tab before allocating the note: phase 2 of the note
	// allocation below needs to know how much the viewport needs, and
	// measurement depends only on activeIdx, not on how much room the note
	// leaves.
	widths := make([]int, len(currentTabs))
	rendered := make([]string, len(currentTabs))
	for i, tab := range currentTabs {
		rendered[i] = m.renderAgentTab(i, tab, activeIdx)
		widths[i] = lipgloss.Width(rendered[i])
	}

	// --- Zone 4: note reservation, phase 1 (its guaranteed minimum) ---
	var note string
	if m.workspace != nil {
		note = m.workspace.Note
	}
	noteFull := noteWidth(note, width) // one-third-capped desired width
	gap := 0
	if noteFull > 0 {
		gap = 1 // one cell between "+ New Agent" and the note
	}

	// A set note always keeps at least this much, so it is never fully hidden.
	nWidth := noteFull
	if nWidth > minNoteWidth {
		nWidth = minNoteWidth
	}

	// noteWidth caps against the full content width and cannot know how many
	// cells the pinned chrome already claimed. Clamp into what the chrome left,
	// so the assembled line never exceeds the pane and tears the border.
	room := width - infoWidth - plusWidth - gap
	if room < 0 {
		room = 0
	}
	if nWidth > room {
		nWidth = room
	}
	if nWidth == 0 {
		gap = 0
	}

	// --- Zone 2: the viewport gets what is left after the note's minimum ---
	avail := width - infoWidth - plusWidth - nWidth - gap
	if avail < 0 {
		avail = 0
	}

	// Pull the viewport to the active tab only when the active tab changed.
	// renderTabBar runs on every View(), so an unconditional pull would undo
	// a manual arrow scroll on the very next repaint.
	//
	// Identity, not index: closing the active tab slides a different tab into
	// the same index, and that must still pull.
	var activeID TabID
	if activeIdx >= 0 && activeIdx < len(currentTabs) {
		activeID = currentTabs[activeIdx].ID
	}
	pullTarget := activeIdx
	if activeID == m.lastRenderedActiveID {
		pullTarget = -1
	}
	// Only record a real agent tab. While the Info tab is active, activeIdx is
	// -1 and activeID is "", and storing that would make the return to the
	// SAME agent tab look like a change — force-pulling the viewport and
	// discarding the user's manual scroll.
	if activeID != "" {
		m.lastRenderedActiveID = activeID
	}

	// --- Zone 4, phase 1b: the note yields to the first agent tab. ---
	// Chrome (21) + gap (1) + the note's 8-cell floor + one tab (12) + its
	// arrow (2) needs 44 cells. Below that the floor and the first tab cannot
	// both fit, and the tabs win — but the note never disappears, so the user
	// still knows one is set. Hence a floor of one cell, which renders as "…".
	//
	// Shrink only when it actually buys a visible tab: at contentWidth 36 the
	// widest viewport any visible note allows is 13 cells, one short of the 14
	// a tab needs, so shrinking there would cost legibility and gain nothing.
	if nWidth > 1 && len(widths) > 0 && m.visibleTabCount(widths, avail, pullTarget) == 0 {
		for w := nWidth - 1; w >= 1; w-- {
			a := width - infoWidth - plusWidth - w - gap
			if a < 0 {
				a = 0
			}
			if m.visibleTabCount(widths, a, pullTarget) > 0 {
				nWidth, avail = w, a
				break
			}
		}
	}

	// --- Zone 4, phase 2: the note grows into whatever the tabs do not need.
	// Bounded by (avail - tabsNeed), so growing can never push the tabs into
	// needing scroll arrows. The agent tabs are never sacrificed for the note.
	tabsNeed := 0
	for _, w := range widths {
		tabsNeed += w
	}
	if spare := avail - tabsNeed; spare > 0 && nWidth < noteFull {
		grow := noteFull - nWidth
		if grow > spare {
			grow = spare
		}
		nWidth += grow
		avail -= grow
	}

	vp := visibleTabs(widths, avail, m.tabScrollOffset, pullTarget)
	m.tabScrollOffset = vp.start

	// fitRun budgets arrowWidth per arrow out of avail, so the visible tabs
	// plus their arrows always fit whenever the arrows themselves fit. When
	// avail is smaller than the arrows alone, drawing them would overflow the
	// pane — and the affordance is useless at that width anyway.
	arrowCells := 0
	if vp.showPrev {
		arrowCells += arrowWidth
	}
	if vp.showNext {
		arrowCells += arrowWidth
	}
	if arrowCells > avail {
		vp.showPrev = false
		vp.showNext = false
	}

	// --- Assemble left-to-right, tracking hit regions against x. ---
	var segments []string
	x := 0

	m.addHit(tabHitInfo, -1, x, infoWidth)
	segments = append(segments, infoRendered)
	x += infoWidth

	if vp.showPrev {
		arrow := m.styles.Muted.Render("‹ ")
		m.addHit(tabHitPrev, -1, x, arrowWidth)
		segments = append(segments, arrow)
		x += arrowWidth
	}

	for i := vp.start; i < vp.end; i++ {
		m.addHit(tabHitTab, i, x, widths[i])
		m.addCloseHit(i, x, widths[i])
		segments = append(segments, rendered[i])
		x += widths[i]
	}

	if vp.showNext {
		arrow := m.styles.Muted.Render("› ")
		m.addHit(tabHitNext, -1, x, arrowWidth)
		segments = append(segments, arrow)
		x += arrowWidth
	}

	if plusWidth > 0 {
		m.addHit(tabHitPlus, -1, x, plusWidth)
		segments = append(segments, plusRendered)
		x += plusWidth
	}

	// --- Session-id badge, drawn only out of leftover slack. ---
	// Temporary instrumentation: a tab's Claude session id changes underneath
	// it (a SessionStart adoption, /clear, a restart's resume), and showing it
	// is how we watch that happen. It is deliberately outside the width
	// budgeting above — it takes only what the tabs and the note left unused,
	// so it can never push a tab out of view or truncate the note.
	var badge string
	badgeWidth, badgeGap := 0, 0
	if slack := width - x - nWidth; slack > 0 {
		reserve := slack
		if nWidth > 0 {
			reserve-- // keep one cell between the note and the badge
		}
		if badge = m.sessionIDBadge(reserve); badge != "" {
			badgeWidth = lipgloss.Width(badge)
			if nWidth > 0 {
				badgeGap = 1
			}
		}
	}

	// --- Right-align note then badge against the content edge. The badge goes
	// last so the session id is always the rightmost thing on the line. ---
	if nWidth > 0 || badgeWidth > 0 {
		pad := width - x - nWidth - badgeGap - badgeWidth
		if pad < 0 {
			pad = 0
		}
		segments = append(segments, strings.Repeat(" ", pad))
		x += pad
	}

	if nWidth > 0 {
		noteStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary)
		truncated := ansi.Truncate(note, nWidth, "…")
		m.addHit(tabHitNote, -1, x, nWidth)
		segments = append(segments, noteStyle.Render(truncated))
		x += nWidth
	}

	if badgeWidth > 0 {
		if badgeGap > 0 {
			segments = append(segments, " ")
			x += badgeGap
		}
		segments = append(segments, badge)
		x += badgeWidth
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Bottom, segments...)

	separatorStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	separatorLine := separatorStyle.Render(strings.Repeat("─", width))

	return tabLine + "\n" + separatorLine
}

// sessionIDBadge renders the active agent tab's Claude session id, or "" when
// there is no id to show or avail cells cannot hold it. The full id is shown
// when it fits, since that is what the logs and the transcript filenames carry;
// below that it degrades to the leading 8 characters, which is still enough to
// see the id change.
func (m *Model) sessionIDBadge(avail int) string {
	if m.infoTabActive {
		return ""
	}
	tabs := m.getTabs()
	idx := m.getActiveTabIdx()
	if idx < 0 || idx >= len(tabs) || tabs[idx] == nil {
		return ""
	}
	tab := tabs[idx]
	tab.mu.Lock()
	id := tab.ClaudeSessionID
	tab.mu.Unlock()
	if id == "" {
		return ""
	}

	switch {
	case avail >= len(id):
	case avail >= shortSessionIDLen && len(id) > shortSessionIDLen:
		id = id[:shortSessionIDLen]
	default:
		return ""
	}
	return m.styles.Muted.Render(id)
}

// shortSessionIDLen is how much of a session id the badge keeps when the full
// one does not fit — a UUID's first group, unique enough to track changes.
const shortSessionIDLen = 8

// visibleTabCount reports how many whole agent tabs fit in avail cells.
func (m *Model) visibleTabCount(widths []int, avail, pullTarget int) int {
	vp := visibleTabs(widths, avail, m.tabScrollOffset, pullTarget)
	return vp.end - vp.start
}

// addHit appends a single-row hit region at x of the given width.
func (m *Model) addHit(kind tabHitKind, index, x, w int) {
	if w <= 0 {
		return
	}
	m.tabHits = append(m.tabHits, tabHit{
		kind:   kind,
		index:  index,
		region: common.HitRegion{X: x, Y: 0, Width: w, Height: 1},
	})
}

// addCloseHit registers the × region, anchored from the tab's right edge.
func (m *Model) addCloseHit(i, x, renderedWidth int) {
	style := m.styles.Tab
	if i == m.getActiveTabIdx() && !m.infoTabActive {
		style = m.styles.ActiveTab
	}
	frameX, _ := style.GetFrameSize()
	leftFrame := frameX / 2

	closeWidth := lipgloss.Width(m.styles.Muted.Render("×")) + 1
	closeX := x + renderedWidth - leftFrame - closeWidth
	if closeX > x {
		m.tabHits = append(m.tabHits, tabHit{
			kind:   tabHitClose,
			index:  i,
			region: common.HitRegion{X: closeX, Y: 0, Width: renderedWidth - (closeX - x), Height: 1},
		})
	}
}

func (m *Model) handleTabBarClick(msg tea.MouseClickMsg) tea.Cmd {
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)

	tabBarY := borderTop + m.infoBarHeight()
	if msg.Y != tabBarY {
		return nil
	}
	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	if localX < 0 {
		return nil
	}
	return m.dispatchTabHit(localX)
}

// scrollTabs moves the tab strip by delta tabs, clamped to the tab count.
func (m *Model) scrollTabs(delta int) {
	n := len(m.getTabs())
	if n == 0 {
		m.tabScrollOffset = 0
		return
	}
	off := m.tabScrollOffset + delta
	if off < 0 {
		off = 0
	}
	if off > n-1 {
		off = n - 1
	}
	m.tabScrollOffset = off
}

// dispatchTabHit resolves a tab-bar-local X coordinate to an action.
// Close buttons are checked first because they overlap tab regions.
func (m *Model) dispatchTabHit(localX int) tea.Cmd {
	const localY = 0

	for _, hit := range m.tabHits {
		if hit.kind == tabHitClose && hit.region.Contains(localX, localY) {
			idx := hit.index
			return func() tea.Msg { return messages.CloseTabAt{Index: idx} }
		}
	}

	for _, hit := range m.tabHits {
		if !hit.region.Contains(localX, localY) {
			continue
		}
		switch hit.kind {
		case tabHitPrev:
			m.scrollTabs(-1)
			return nil
		case tabHitNext:
			m.scrollTabs(1)
			return nil
		case tabHitNote:
			ws := m.workspace
			if ws == nil {
				return nil
			}
			return func() tea.Msg {
				return messages.ShowSetWorkspaceNoteDialog{Workspace: ws}
			}
		case tabHitPlus:
			if m.workspace != nil && m.workspace.Archived() {
				ws := m.workspace
				return func() tea.Msg {
					return messages.ShowArchivedWorkspaceDialog{Workspace: ws}
				}
			}
			return func() tea.Msg { return messages.ShowCustomizeTabDialog{} }
		case tabHitInfo:
			m.infoTabActive = true
			return nil
		case tabHitTab:
			if m.workspace != nil && m.workspace.Archived() {
				ws := m.workspace
				return func() tea.Msg {
					return messages.ShowArchivedWorkspaceDialog{Workspace: ws}
				}
			}
			m.infoTabActive = false
			m.setActiveTabIdx(hit.index)
			return m.tabSelectionChangedCmd()
		}
	}
	return nil
}
