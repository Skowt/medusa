package center

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
)

func TestInfoBarLabelsCopyAndShowFeedback(t *testing.T) {
	m, _ := tabBarModel(t, "feature-copy", "", 1, 140, 40)
	ws := data.NewWorkspace("feature-copy", "feature/copy", "main", "/repo", "/repo/feature-copy")
	m.SetWorkspace(ws)
	var copied string
	m.clipboardWrite = func(value string) error {
		copied = value
		return nil
	}

	line := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	if strings.Contains(line, "[Copy]") {
		t.Fatalf("info bar must not retain the copy button: %q", line)
	}

	click := func(kind actionBarButtonKind, want string, target copyTarget) {
		t.Helper()
		for _, hit := range m.actionBarHits {
			if hit.kind != kind {
				continue
			}
			if cmd := m.handleInfoBarClick(hit.region.X, m.actionBarY); cmd == nil {
				t.Fatalf("clicking %v returned no feedback timer", kind)
			}
			if copied != want {
				t.Fatalf("clicking %v copied %q, want %q", kind, copied, want)
			}
			if !m.copyFeedbackActive(target) {
				t.Fatalf("clicking %v did not activate feedback", kind)
			}
			return
		}
		t.Fatalf("missing hit region for %v", kind)
	}

	click(actionBarCopyBranch, ws.Branch(), copyTargetBranch)
	if line := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]; !strings.Contains(line, "✓ copied") {
		t.Fatalf("branch feedback not rendered: %q", line)
	}

	click(actionBarCopyDir, ws.Root(), copyTargetWorkdir)
	if line := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]; strings.Count(line, "✓ copied") < 2 {
		t.Fatalf("branch and workdir feedback not rendered: %q", line)
	}
}

func TestCopyLabelsKeepWidthAcrossHoverAndFeedback(t *testing.T) {
	m, _ := tabBarModel(t, "stable-copy", "", 1, 140, 40)
	ws := data.NewWorkspace("stable-copy", "feature/a", "main", "/repo", "/repo/stable-copy")
	m.SetWorkspace(ws)

	normal := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	m.copyHover, m.copyHoverActive = copyTargetBranch, true
	hovered := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	if !strings.Contains(hovered, "click to copy") {
		t.Fatalf("hover label missing: %q", hovered)
	}
	m.copyHoverActive = false
	m.copyFeedback = map[copyTarget]uint64{copyTargetBranch: 1}
	copied := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	if lipgloss.Width(normal) != lipgloss.Width(hovered) || lipgloss.Width(normal) != lipgloss.Width(copied) {
		t.Fatalf("copy states changed line width: normal=%d hover=%d copied=%d",
			lipgloss.Width(normal), lipgloss.Width(hovered), lipgloss.Width(copied))
	}

	m, _ = tabBarWithSessionID(t, "stable-sid", "", 1, 120, testSessionID)
	normal = strings.SplitN(m.renderTabBar(), "\n", 2)[0]
	m.copyHover, m.copyHoverActive = copyTargetSessionID, true
	hovered = strings.SplitN(m.renderTabBar(), "\n", 2)[0]
	if !strings.Contains(hovered, "click to copy") || lipgloss.Width(normal) != lipgloss.Width(hovered) {
		t.Fatalf("session hover shifted its line: normal=%q hover=%q", normal, hovered)
	}
}

func TestMouseMotionFindsCopyLabels(t *testing.T) {
	m, _ := tabBarModel(t, "hover-copy", "", 1, 140, 40)
	m.SetWorkspace(data.NewWorkspace("hover-copy", "feature/hover", "main", "/repo", "/repo/hover-copy"))
	_ = m.renderInfoBar(140)
	for _, hit := range m.actionBarHits {
		if hit.kind == actionBarCopyBranch {
			m.updateCopyHover(m.offsetX+2+hit.region.X, 1+m.actionBarY)
			if !m.copyHoverActive || m.copyHover != copyTargetBranch {
				t.Fatal("branch hover region was not detected")
			}
			return
		}
	}
	t.Fatal("branch hover region was not rendered")
}

func TestSessionIDCopiesFullValueAndShowsFeedback(t *testing.T) {
	m, _ := tabBarWithSessionID(t, "ws-copy-sid", "", 1, 120, testSessionID)
	var copied string
	m.clipboardWrite = func(value string) error {
		copied = value
		return nil
	}

	hit := findHit(m, tabHitSessionID)
	if hit == nil {
		t.Fatal("session id must register a hit region")
	}
	if cmd := m.dispatchTabHit(hit.region.X); cmd == nil {
		t.Fatal("clicking session id returned no feedback timer")
	}
	if copied != testSessionID {
		t.Fatalf("copied %q, want full session id %q", copied, testSessionID)
	}
	line := strings.SplitN(m.renderTabBar(), "\n", 2)[0]
	if !strings.Contains(line, "✓ copied") || strings.Contains(line, testSessionID) {
		t.Fatalf("session id feedback not rendered: %q", line)
	}
}

func TestCopyFeedbackExpiryDoesNotClearNewerFeedback(t *testing.T) {
	m := newTestModel()
	m.copyFeedback = map[copyTarget]uint64{copyTargetBranch: 2}

	m.Update(copyFeedbackExpired{target: copyTargetBranch, generation: 1})
	if !m.copyFeedbackActive(copyTargetBranch) {
		t.Fatal("stale expiry cleared newer copy feedback")
	}
	m.Update(copyFeedbackExpired{target: copyTargetBranch, generation: 2})
	if m.copyFeedbackActive(copyTargetBranch) {
		t.Fatal("current expiry did not clear copy feedback")
	}
}
