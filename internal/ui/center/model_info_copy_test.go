package center

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// infoTabModel builds a model showing the Info tab for ws, with content laid
// out the way renderWorkspaceInfo does: through Label() and InfoCopyValue.
// displayPath stands in for the ~-shortened path the Info tab prints.
func infoTabModel(t *testing.T, ws *data.Workspace, displayPath string) *Model {
	t.Helper()
	m := newTestModel()
	m.SetWorkspace(ws)
	m.SetSize(140, 40)
	m.SelectInfoTab()
	m.SetInfoContent(infoContent(m, ws.Branch(), displayPath))
	return m
}

func infoContent(m *Model, branch, displayPath string) string {
	branchText, _ := m.InfoCopyValue(InfoCopyBranch, branch)
	pathText, _ := m.InfoCopyValue(InfoCopyPath, displayPath)
	return "Notes\n  (no note)\n\n" +
		InfoCopyBranch.Label() + branchText + " [Rename]\n" +
		InfoCopyPath.Label() + pathText + "\n"
}

// infoScreenXY converts an Info-tab content column/row into the screen
// coordinates the mouse handlers receive.
func infoScreenXY(m *Model, x, infoY int) (int, int) {
	const borderLeft, paddingLeft, borderTop = 1, 1, 1
	return m.offsetX + borderLeft + paddingLeft + x, borderTop + m.infoContentOriginY() + infoY
}

func TestInfoTabValuesAreHoverable(t *testing.T) {
	ws := data.NewWorkspace("info-hover", "feature/hover", "main", "/repo", "/home/u/ws/info-hover")
	m := infoTabModel(t, ws, "~/ws/info-hover")

	for _, tc := range []struct {
		name  string
		field InfoCopyField
		want  copyTarget
	}{
		{"branch", InfoCopyBranch, copyTargetInfoBranch},
		{"path", InfoCopyPath, copyTargetInfoPath},
	} {
		region, ok := m.infoCopyRegion(tc.field)
		if !ok {
			t.Fatalf("%s: no hit region for the rendered value", tc.name)
		}
		m.copyHoverActive = false
		m.updateCopyHover(infoScreenXY(m, region.X, region.Y))
		if !m.copyHoverActive || m.copyHover != tc.want {
			t.Fatalf("%s: hover not detected (active=%v target=%v)", tc.name, m.copyHoverActive, m.copyHover)
		}

		// Moving off the value clears the affordance.
		m.updateCopyHover(infoScreenXY(m, region.X+region.Width, region.Y))
		if m.copyHoverActive {
			t.Fatalf("%s: hover stuck past the end of the value", tc.name)
		}
	}
}

func TestInfoTabValuesCopyOnClick(t *testing.T) {
	ws := data.NewWorkspace("info-copy", "feature/copy", "main", "/repo", "/home/u/ws/info-copy")
	m := infoTabModel(t, ws, "~/ws/info-copy")
	var copied string
	m.clipboardWrite = func(value string) error {
		copied = value
		return nil
	}

	click := func(field InfoCopyField, want string) {
		t.Helper()
		region, ok := m.infoCopyRegion(field)
		if !ok {
			t.Fatalf("no hit region for field %v", field)
		}
		x, y := infoScreenXY(m, region.X, region.Y)
		cmd := m.handleInfoContentClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if cmd == nil {
			t.Fatalf("clicking field %v returned no feedback timer", field)
		}
		if copied != want {
			t.Fatalf("clicking field %v copied %q, want %q", field, copied, want)
		}
	}

	click(InfoCopyBranch, ws.Branch())
	if !m.copyFeedbackActive(copyTargetInfoBranch) {
		t.Fatal("branch click did not activate feedback")
	}
	// The path is displayed shortened but must copy in full.
	click(InfoCopyPath, ws.Root())
	if !m.copyFeedbackActive(copyTargetInfoPath) {
		t.Fatal("path click did not activate feedback")
	}
}

func TestInfoTabCopyStatesKeepLineWidth(t *testing.T) {
	ws := data.NewWorkspace("info-width", "a", "main", "/repo", "/home/u/ws/info-width")
	m := infoTabModel(t, ws, "~/w")

	widths := func() (int, int) {
		lines := strings.Split(m.infoContent, "\n")
		var branch, path int
		for _, line := range lines {
			stripped := ansi.Strip(line)
			switch {
			case strings.HasPrefix(stripped, InfoCopyBranch.Label()):
				branch = lipgloss.Width(stripped)
			case strings.HasPrefix(stripped, InfoCopyPath.Label()):
				path = lipgloss.Width(stripped)
			}
		}
		return branch, path
	}

	wantBranch, wantPath := widths()
	for _, state := range []func(){
		func() { m.copyHover, m.copyHoverActive = copyTargetInfoBranch, true },
		func() { m.copyHover, m.copyHoverActive = copyTargetInfoPath, true },
		func() {
			m.copyHoverActive = false
			m.copyFeedback = map[copyTarget]uint64{copyTargetInfoBranch: 1, copyTargetInfoPath: 1}
		},
	} {
		state()
		m.SetInfoContent(infoContent(m, ws.Branch(), "~/w"))
		gotBranch, gotPath := widths()
		if gotBranch != wantBranch || gotPath != wantPath {
			t.Fatalf("copy state moved [Rename] / line end: branch %d→%d, path %d→%d",
				wantBranch, gotBranch, wantPath, gotPath)
		}
	}
}

func TestInfoTabRenameStillClickable(t *testing.T) {
	ws := data.NewWorkspace("info-rename", "feature/rename", "main", "/repo", "/home/u/ws/info-rename")
	m := infoTabModel(t, ws, "~/ws/info-rename")

	region, ok := m.infoCopyRegion(InfoCopyBranch)
	if !ok {
		t.Fatal("no branch region")
	}
	// [Rename] sits one space past the padded branch value.
	x, y := infoScreenXY(m, region.X+region.Width+1, region.Y)
	cmd := m.handleInfoContentClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("clicking [Rename] returned nothing")
	}
	if _, ok := cmd().(messages.ShowRenameWorkspaceDialog); !ok {
		t.Fatalf("clicking [Rename] produced %T, want ShowRenameWorkspaceDialog", cmd())
	}
}
