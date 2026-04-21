package dashboard

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

// TestClickRouting_DuplicateIconTriggersDuplicate verifies that the icon
// positions recorded while rendering a selected workspace row correspond to
// the two-column gap of the " + × " slot layout.
func TestClickRouting_DuplicateIconTriggersDuplicate(t *testing.T) {
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
	_ = m.renderWorkspaceRow(m.rows[wsIdx], true) // triggers the selected branch to record icon positions

	if m.duplicateIconX == 0 || m.deleteIconX == 0 {
		t.Fatalf("icon positions not set: dup=%d del=%d", m.duplicateIconX, m.deleteIconX)
	}
	if m.deleteIconX <= m.duplicateIconX {
		t.Fatalf("deleteIconX (%d) must be to the right of duplicateIconX (%d)", m.deleteIconX, m.duplicateIconX)
	}
	// The two icons should be 2 columns apart in the " + × " layout:
	// dup at nameEnd+1, del at nameEnd+3.
	if m.deleteIconX-m.duplicateIconX != 2 {
		t.Errorf("expected delete-duplicate gap of 2 cols, got %d", m.deleteIconX-m.duplicateIconX)
	}
}
