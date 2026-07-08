package dashboard

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

// TestClickRouting_ButtonHitsRecordedOnFooter verifies that rendering a
// selected workspace row records the three action-button hit boxes on the
// footer line, ordered dupe < group < archive.
func TestClickRouting_ButtonHitsRecordedOnFooter(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)),
	}
	m.width = 40
	m.height = 20
	m.rebuildRows()

	wsIdx := -1
	for i, r := range m.rows {
		if r.Type == RowWorkspace {
			wsIdx = i
			break
		}
	}
	if wsIdx == -1 {
		t.Fatal("expected a workspace row")
	}
	m.cursor = wsIdx
	_ = m.renderWorkspaceRow(m.rows[wsIdx], true) // records button hits

	if len(m.wsButtonHits) != 3 {
		t.Fatalf("expected 3 button hits, got %d: %+v", len(m.wsButtonHits), m.wsButtonHits)
	}
	// All three share the footer line.
	line := m.wsButtonHits[0].line
	for _, h := range m.wsButtonHits {
		if h.line != line {
			t.Errorf("button %d on line %d, expected all on %d", h.action, h.line, line)
		}
	}
	if m.wsButtonHits[0].action != btnDuplicate || m.wsButtonHits[1].action != btnGroup || m.wsButtonHits[2].action != btnArchive {
		t.Errorf("unexpected button order: %+v", m.wsButtonHits)
	}
	if m.wsButtonHits[0].x0 >= m.wsButtonHits[1].x0 || m.wsButtonHits[1].x0 >= m.wsButtonHits[2].x0 {
		t.Errorf("buttons must be left-to-right: %+v", m.wsButtonHits)
	}
	// The footer is the last line of a selected single-repo row (name+meta+footer).
	if line != m.rowLineCount(wsIdx)-1 {
		t.Errorf("footer line %d, expected last row line %d", line, m.rowLineCount(wsIdx)-1)
	}
}
