package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestDialogCursorHiddenWhenNotVisible(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")
	if c := d.Cursor(); c != nil {
		t.Fatalf("expected nil cursor when dialog is hidden, got %+v", c)
	}
}

func TestDialogCursorPositionInput(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")
	d.Show()
	d.input.SetValue("abc")
	d.input.SetCursor(3)

	inputCursor := d.input.Cursor()
	if inputCursor == nil {
		t.Fatalf("expected input cursor, got nil")
	}

	c := d.Cursor()
	if c == nil {
		t.Fatalf("expected dialog cursor, got nil")
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	prefix := titleStyle.Render(d.title) + "\n"

	expectedX := inputCursor.X + 3
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 1

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}

// clickRegion sends a left-click at the centre of the named region and
// returns the resulting tea.Cmd. It exercises the same code path as a real
// mouse click — the dialog's build() output is the source of truth, so any
// drift between rendered rows and click regions surfaces here.
func clickRegion(t *testing.T, d *Dialog, regionID string) tea.Cmd {
	t.Helper()
	b := d.build()
	region, ok := b.RegionByID(regionID)
	if !ok {
		t.Fatalf("region %q not found in dialog build", regionID)
	}
	dialogW, dialogH := b.Size()
	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	contentX, contentY := b.ContentOffset()
	screenX := dialogX + contentX + region.X + region.Width/2
	screenY := dialogY + contentY + region.Y + region.Height/2
	msg := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := d.Update(msg)
	return cmd
}

func TestDialogConfirmClickYes(t *testing.T) {
	d := NewConfirmDialog("quit", "Quit?", "Are you sure you want to quit?")
	d.SetSize(80, 24)
	d.Show()

	cmd := clickRegion(t, d, dialogIDOptPrefix+"0")
	if cmd == nil {
		t.Fatalf("expected command from clicking Yes button, got nil")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", cmd())
	}
	if res.ID != "quit" || !res.Confirmed {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestDialogConfirmClickNo(t *testing.T) {
	d := NewConfirmDialog("quit", "Quit?", "Are you sure you want to quit?")
	d.SetSize(80, 24)
	d.Show()

	cmd := clickRegion(t, d, dialogIDOptPrefix+"1")
	if cmd == nil {
		t.Fatalf("expected command from clicking No button, got nil")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", cmd())
	}
	if res.ID != "quit" || res.Confirmed {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestDialogInputClickCancel(t *testing.T) {
	d := NewInputDialog("create_workspace", "Create Worktree", "Enter worktree name...")
	d.SetSize(80, 24)
	d.Show()
	d.input.SetValue("feature-1")

	cmd := clickRegion(t, d, dialogIDCancel)
	if cmd == nil {
		t.Fatalf("expected command from clicking Cancel button, got nil")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", cmd())
	}
	if res.ID != "create_workspace" || res.Confirmed {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestDialogInputClickCancelAfterLongDescriptions reproduces the bug that
// motivated the LineBuilder migration: descriptions that wrap to 3+ lines
// previously drifted the row count out of sync with hit regions, so Cancel
// was no longer where the click handler thought. Now build() is the single
// source of truth, so this stays accurate regardless of description length.
func TestDialogInputClickCancelAfterLongDescriptions(t *testing.T) {
	d := NewInputDialog("customize", "New Claude Tab", "")
	d.SetInputHidden(true)
	d.SetMessage("Configure settings for this tab.")
	d.SetSelect("Starting Mode:", []SelectOption{
		{Value: "auto", Label: "Auto", Description: "Auto-approves tool calls; a background classifier checks each action against your request before allowing it to run."},
		{Value: "plan", Label: "Plan", Description: "Read-only exploration."},
	}, "auto")
	d.SetCheckbox("Sandboxed", true)
	d.SetCheckboxDescription(1, "Sandboxes subprocess calls including Bash commands. Tool use does not use sandbox (e.g. Write, Edit). Long descriptions used to drift the click target.")
	d.SetCheckbox2("Allow unsandboxed commands", false)
	d.SetCheckboxDescription(2, "Allows Claude to try run blocked commands outside of the sandbox, using the user's allowed permissions. Do not use in 'Bypass Permissions' mode.")
	d.SetCheckbox2RequiresFirst(true)
	d.SetSize(120, 40)
	d.Show()

	cmd := clickRegion(t, d, dialogIDCancel)
	if cmd == nil {
		t.Fatalf("expected command from clicking Cancel after long descriptions, got nil")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", cmd())
	}
	if res.ID != "customize" || res.Confirmed {
		t.Fatalf("unexpected result: %+v", res)
	}
}
