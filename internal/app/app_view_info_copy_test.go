package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/ui/center"
)

// TestWorkspaceInfoValuesAreCopyFields pins the Info tab's Branch and Path rows
// to the contract center's hit test relies on: the padded label at column 0,
// the value immediately after it, and no [Copy] button. Drift on either side
// renders a value that cannot be clicked.
func TestWorkspaceInfoValuesAreCopyFields(t *testing.T) {
	app, cfg := newTestApp(t)
	ws := data.NewWorkspace("info-tab", "feature/info", "main", "/repo", cfg.Paths.WorkspacesRoot+"/info-tab")
	app.activeWorkspace = ws

	content := ansi.Strip(app.renderWorkspaceInfo())
	if strings.Contains(content, "[Copy]") {
		t.Errorf("Info tab still renders a [Copy] button:\n%s", content)
	}
	if !strings.Contains(content, "[Rename]") {
		t.Errorf("Info tab lost its [Rename] button:\n%s", content)
	}

	lineWith := func(label string) (string, bool) {
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, label) {
				return line, true
			}
		}
		return "", false
	}

	branchValue, _ := app.center.InfoCopyValue(center.InfoCopyBranch, ws.Branch())
	line, ok := lineWith(center.InfoCopyBranch.Label())
	if !ok {
		t.Fatalf("no branch line starting with %q:\n%s", center.InfoCopyBranch.Label(), content)
	}
	if want := center.InfoCopyBranch.Label() + branchValue + " [Rename]"; line != want {
		t.Errorf("branch line = %q, want %q", line, want)
	}

	if _, ok := lineWith(center.InfoCopyPath.Label()); !ok {
		t.Fatalf("no path line starting with %q:\n%s", center.InfoCopyPath.Label(), content)
	}
}
