package center

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

const testSessionID = "674a87b8-45ac-48f0-adb0-63fadc2e9cd6"

// tabBarWithSessionID renders the tab bar with a session id on the active tab
// and returns the model plus the rendered tab line (without the separator).
func tabBarWithSessionID(t *testing.T, name, note string, tabCount, w int, sid string) (*Model, string) {
	t.Helper()
	m, _ := tabBarModel(t, name, note, tabCount, w, 40)
	tabs := m.getTabs()
	if len(tabs) > 0 {
		tabs[0].ClaudeSessionID = sid
	}
	rendered := m.renderTabBar()
	return m, strings.SplitN(rendered, "\n", 2)[0]
}

// The badge shows the whole id when the pane is wide enough: it is compared
// against log lines and transcript filenames, which carry the full UUID.
func TestSessionIDBadgeShowsFullID(t *testing.T) {
	_, line := tabBarWithSessionID(t, "ws-sid", "", 2, 160, testSessionID)
	if !strings.Contains(line, testSessionID) {
		t.Errorf("wide pane must show the full session id, got: %q", line)
	}
}

// Below that it degrades to the first UUID group rather than disappearing —
// enough to watch the id change.
func TestSessionIDBadgeShortensBeforeDisappearing(t *testing.T) {
	short := testSessionID[:shortSessionIDLen]
	var sawShort bool
	for w := 40; w <= 160; w += 2 {
		_, line := tabBarWithSessionID(t, "ws-sid-short", "", 2, w, testSessionID)
		full := strings.Contains(line, testSessionID)
		if !full && strings.Contains(line, short) {
			sawShort = true
		}
	}
	if !sawShort {
		t.Error("expected some width to show the shortened id")
	}
}

// The badge is drawn out of slack only. At no width may it displace a tab,
// shrink the note, or push the line past the pane.
func TestSessionIDBadgeNeverStealsWidth(t *testing.T) {
	const note = "Fix the auth redirect loop"
	for _, paneW := range []int{28, 32, 36, 40, 44, 48, 60, 80, 120, 200} {
		for _, tabCount := range []int{1, 4} {
			plain, plainLine := tabBarWithSessionID(t, "ws-plain", note, tabCount, paneW, "")
			withID, idLine := tabBarWithSessionID(t, "ws-plain", note, tabCount, paneW, testSessionID)

			if got, want := countHits(withID, tabHitTab), countHits(plain, tabHitTab); got != want {
				t.Errorf("pane=%d tabs=%d: badge changed visible tab count (%d, want %d)",
					paneW, tabCount, got, want)
			}
			plainNote, idNote := findHit(plain, tabHitNote), findHit(withID, tabHitNote)
			if (plainNote == nil) != (idNote == nil) {
				t.Errorf("pane=%d tabs=%d: badge changed whether the note renders", paneW, tabCount)
			} else if plainNote != nil && plainNote.region.Width != idNote.region.Width {
				t.Errorf("pane=%d tabs=%d: badge shrank the note (%d → %d)",
					paneW, tabCount, plainNote.region.Width, idNote.region.Width)
			}
			if w := lipgloss.Width(idLine); w > withID.contentWidth() {
				t.Errorf("pane=%d tabs=%d: tab line is %d cells, pane holds %d",
					paneW, tabCount, w, withID.contentWidth())
			}
			// The badge takes the content edge and pushes the note left, so
			// the plain line's width is the badge-free floor.
			if idNote != nil && lipgloss.Width(idLine) < lipgloss.Width(plainLine) {
				t.Errorf("pane=%d tabs=%d: badge shortened the line", paneW, tabCount)
			}
			if plainNote != nil && idNote != nil && idNote.region.X > plainNote.region.X {
				t.Errorf("pane=%d tabs=%d: note moved right (%d → %d); the badge must sit to its right",
					paneW, tabCount, plainNote.region.X, idNote.region.X)
			}
		}
	}
}

// The Info tab has no conversation of its own, so it shows no badge.
func TestSessionIDBadgeHiddenForInfoTab(t *testing.T) {
	m, _ := tabBarWithSessionID(t, "ws-info", "", 2, 160, testSessionID)
	m.infoTabActive = true
	line := strings.SplitN(m.renderTabBar(), "\n", 2)[0]
	if strings.Contains(line, testSessionID[:shortSessionIDLen]) {
		t.Errorf("Info tab must not show a session id badge, got: %q", line)
	}
}

func countHits(m *Model, kind tabHitKind) int {
	var n int
	for _, h := range m.tabHits {
		if h.kind == kind {
			n++
		}
	}
	return n
}

// The session id is the rightmost thing on the tab line: a note is laid out to
// its left, never after it, so the id stays column-aligned as notes change.
func TestSessionIDBadgeSitsRightOfTheNote(t *testing.T) {
	const note = "Testing locally"
	for _, paneW := range []int{80, 120, 160, 200} {
		m, line := tabBarWithSessionID(t, "ws-order", note, 2, paneW, testSessionID)
		h := findHit(m, tabHitNote)
		if h == nil {
			t.Fatalf("pane=%d: note must render", paneW)
		}
		id := testSessionID
		if !strings.Contains(line, id) {
			id = id[:shortSessionIDLen]
		}
		noteAt, idAt := strings.Index(line, note), strings.LastIndex(line, id)
		if noteAt < 0 || idAt < 0 {
			t.Fatalf("pane=%d: want both note and id on the line, got: %q", paneW, line)
		}
		if idAt < noteAt {
			t.Errorf("pane=%d: session id must follow the note, got: %q", paneW, line)
		}
	}
}
