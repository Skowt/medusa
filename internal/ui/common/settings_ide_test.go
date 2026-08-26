package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newIDESettingsDialog(alwaysOpen bool) *SettingsDialog {
	d := NewSettingsDialog(ThemeTokyoNight,
		false, false, false, false, "", false, alwaysOpen,
	)
	d.SetSize(120, 60)
	d.Show()
	return d
}

// TestSettingsCarriesTheIDEPreferenceBothWays guards the way back: the picker
// can only ever turn the prompt off, so Settings is the sole path that turns it
// on again, and a Save that dropped the flag either way would strand the user.
func TestSettingsCarriesTheIDEPreferenceBothWays(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial bool
	}{
		{"turning it off", true},
		{"turning it on", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newIDESettingsDialog(tc.initial)

			d.focusedItem = settingsItemIDEAlwaysOpen
			if _, cmd := d.handleSelect(); cmd != nil {
				t.Fatal("toggling the IDE checkbox emitted a command")
			}
			d.focusedItem = settingsItemSave
			_, cmd := d.handleSelect()
			res, ok := cmd().(SettingsResult)
			if !ok {
				t.Fatalf("save produced %T, want SettingsResult", cmd())
			}
			if res.IDEAlwaysOpen == tc.initial {
				t.Errorf("IDEAlwaysOpen = %v, want %v", res.IDEAlwaysOpen, !tc.initial)
			}
		})
	}
}

// TestSettingsNamesTheRememberedIDE keeps the label concrete: a checkbox that
// says which IDE it will launch is the only place the remembered choice is
// visible outside the picker.
func TestSettingsNamesTheRememberedIDE(t *testing.T) {
	d := newIDESettingsDialog(true)
	d.SetIDEName("Cursor")
	if view := flatten(d.View()); !strings.Contains(view, "Always open Cursor without asking") {
		t.Errorf("settings did not name the remembered IDE:\n%s", view)
	}

	d = newIDESettingsDialog(true)
	if view := flatten(d.View()); !strings.Contains(view, "Always open the last IDE without asking") {
		t.Errorf("settings with no remembered IDE rendered no fallback label:\n%s", view)
	}
}

// TestSettingsIDECheckboxIsKeyboardReachable guards the enum ordering: Tab
// walks settingsItem values, so a row rendered without a slot in that walk is
// mouse-only.
func TestSettingsIDECheckboxIsKeyboardReachable(t *testing.T) {
	d := newIDESettingsDialog(false)
	for i := 0; i <= int(settingsItemClose); i++ {
		if d.focusedItem == settingsItemIDEAlwaysOpen {
			if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
				t.Fatal("enter on the IDE checkbox emitted a command")
			}
			if !d.ideAlwaysOpen {
				t.Fatal("enter on the focused IDE checkbox did not tick it")
			}
			return
		}
		d, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	t.Fatal("tab never reached the IDE checkbox")
}
