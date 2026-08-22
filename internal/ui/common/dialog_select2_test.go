package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func twoSelectDialog() *Dialog {
	d := NewInputDialog("new-tab", "New Tab", "")
	d.SetInputHidden(true)
	d.SetSelect("Assistant:", []SelectOption{
		{Value: "claude", Label: "Claude Code"},
		{Value: "codex", Label: "Codex"},
	}, "claude")
	d.SetSelectNotifiesChange(0)
	d.SetSelect2("Sandbox:", []SelectOption{
		{Value: "workspace-write", Label: "Workspace Write"},
		{Value: "read-only", Label: "Read Only"},
	}, "workspace-write")
	d.SetSize(80, 24)
	d.Show()
	return d
}

// Both cyclers render, and both reach the caller on submit: reading one slot's
// value out of the other would launch an agent with another agent's flags.
func TestTwoSelectsRenderAndSubmit(t *testing.T) {
	d := twoSelectDialog()
	view := d.View()
	for _, want := range []string{"Assistant:", "Claude Code", "Sandbox:", "Workspace Write"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
	if got, want := d.SelectValue(), "claude"; got != want {
		t.Errorf("SelectValue = %q, want %q", got, want)
	}
	if got, want := d.Select2Value(), "workspace-write"; got != want {
		t.Errorf("Select2Value = %q, want %q", got, want)
	}
}

// Slot order is render order: slot 0 above slot 1.
func TestSelectSlotsRenderInOrder(t *testing.T) {
	view := twoSelectDialog().View()
	if strings.Index(view, "Assistant:") > strings.Index(view, "Sandbox:") {
		t.Errorf("slot 0 must render above slot 1:\n%s", view)
	}
}

// The arrow keys act on the focused cycler only — with two on screen, moving
// the wrong one is a silent misconfiguration.
func TestArrowsCycleTheFocusedSelectOnly(t *testing.T) {
	d := twoSelectDialog()
	d.FocusSelect(1)

	d, _ = d.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if got := d.Select2Value(); got != "read-only" {
		t.Errorf("focused slot 1 did not cycle: %q", got)
	}
	if got := d.SelectValue(); got != "claude" {
		t.Errorf("unfocused slot 0 cycled too: %q", got)
	}
}

// A slot marked with SetSelectNotifiesChange reports the change immediately, so
// its owner can rebuild the dialog around the new value.
func TestNotifyingSelectEmitsChange(t *testing.T) {
	d := twoSelectDialog()
	d.FocusSelect(0)

	_, cmd := d.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd == nil {
		t.Fatal("cycling a notifying select produced no message")
	}
	changed, ok := cmd().(DialogSelectChanged)
	if !ok {
		t.Fatalf("got %T, want DialogSelectChanged", cmd())
	}
	if changed.ID != "new-tab" || changed.Slot != 0 || changed.Value != "codex" {
		t.Errorf("change = %+v, want the new slot-0 value", changed)
	}
}

// A slot that did not ask for notification stays silent until submit.
func TestSilentSelectEmitsNothing(t *testing.T) {
	d := twoSelectDialog()
	d.FocusSelect(1)

	_, cmd := d.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd != nil {
		t.Errorf("slot 1 emitted %T without opting in", cmd())
	}
}

// Cycling a single-option select is not a change, so it must not fire.
func TestNotifyOnlyFiresOnAnActualChange(t *testing.T) {
	d := NewInputDialog("one", "One", "")
	d.SetInputHidden(true)
	d.SetSelect("Assistant:", []SelectOption{{Value: "claude", Label: "Claude Code"}}, "claude")
	d.SetSelectNotifiesChange(0)
	d.SetSize(80, 24)
	d.Show()
	d.FocusSelect(0)

	if _, cmd := d.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}); cmd != nil {
		t.Errorf("a wrap onto the same option emitted %T", cmd())
	}
}

func TestDisabledSelectCannotFocusOrCycle(t *testing.T) {
	d := twoSelectDialog()
	d.SetSelectDisabled(1, true)
	want := d.Select2Value()

	d.FocusSelect(1)
	if d.focusedSelectSlot() == 1 {
		t.Fatal("disabled select received focus")
	}
	if cmd := d.cycleSelect(1, 1); cmd != nil {
		t.Fatalf("disabled select emitted %T", cmd)
	}
	if got := d.Select2Value(); got != want {
		t.Errorf("disabled select changed from %q to %q", want, got)
	}
}

// The focus ring reaches both cyclers and the checkboxes alongside them.
func TestFocusRingCoversBothSelects(t *testing.T) {
	d := twoSelectDialog()
	d.SetCheckbox("Ask for approval", true)

	seen := map[int]bool{}
	for i := 0; i < focusSlotCount*2; i++ {
		d.advanceFocus(+1)
		seen[d.currentFocusIdx()] = true
	}
	for _, slot := range []int{1, 4, 5} { // checkbox 1, select 0, select 1
		if !seen[slot] {
			t.Errorf("focus ring never reached slot %d (seen %v)", slot, seen)
		}
	}
}

func TestFocusRingSkipsDisabledSelect(t *testing.T) {
	d := twoSelectDialog()
	d.SetSelectDisabled(1, true)
	for i := 0; i < focusSlotCount*2; i++ {
		d.advanceFocus(+1)
		if d.currentFocusIdx() == 5 {
			t.Fatal("focus ring reached disabled select")
		}
	}
}
