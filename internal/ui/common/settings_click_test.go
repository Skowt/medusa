package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSettingsDialog_ClickHitsIntendedRow is the regression test for the
// vertical misalignment bug where clicking "Hide sidebar" toggled
// "Show keymap hints" and "Hide terminal" toggled "Hide sidebar". Root cause:
// long labels elsewhere in the dialog soft-wrapped, making the composited
// dialog taller than handleClick's dialogBounds math assumed, so the whole
// click map was offset by the number of wraps. With LineBuilder the Y counter
// tracks post-wrap rows and Size() matches viewDimensions of View().
//
// The test exercises every interactive row: it finds the hit region, clicks
// at its geometric centre using the same coordinate math the compositor uses,
// and asserts that the correct toggle fires. A regression of the centering
// bug shows up as one (or more) row-adjacent assertions failing in lockstep.
func TestSettingsDialog_ClickHitsIntendedRow(t *testing.T) {
	const termW, termH = 120, 60

	d := NewSettingsDialog(ThemeTokyoNight,
		false, // keymap
		false, // hideSidebar
		false, // hideTerminal
		false, // autoStartAgent
		false, // syncPlugins
		"",    // sound
		false, // tmuxPersistence
		false, // ideAlwaysOpen
	)
	d.SetSize(termW, termH)
	d.Show()

	// Populate s.hitRegions by building once.
	b := d.build()
	dialogW, dialogH := b.Size()
	dialogX, dialogY := centerOrigin(termW, termH, dialogW, dialogH)
	contentX, contentY := b.ContentOffset()

	clickItem := func(t *testing.T, item settingsItem) {
		t.Helper()
		var reg HitRegion
		found := false
		for _, h := range d.hitRegions {
			if h.item == item {
				reg = h.region
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no hit region found for item %d — dialog must expose every interactive row", item)
		}
		mx := dialogX + contentX + reg.X + reg.Width/2
		my := dialogY + contentY + reg.Y + reg.Height/2
		_, _ = d.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: mx, Y: my})
	}

	// Toggle each checkbox and verify only that flag flipped.
	checkboxCases := []struct {
		name   string
		item   settingsItem
		read   func() bool
		others func() bool // true if any unrelated flag changed from its initial value
	}{
		{"keymap", settingsItemKeymap,
			func() bool { return d.showKeymapHints },
			func() bool { return d.hideSidebar || d.hideTerminal }},
		{"hideSidebar", settingsItemHideSidebar,
			func() bool { return d.hideSidebar },
			func() bool { return d.hideTerminal }},
		{"hideTerminal", settingsItemHideTerminal,
			func() bool { return d.hideTerminal },
			func() bool { return false }},
		{"ideAlwaysOpen", settingsItemIDEAlwaysOpen,
			func() bool { return d.ideAlwaysOpen },
			func() bool { return false }},
		{"syncPlugins", settingsItemSyncPlugins,
			func() bool { return d.syncProfilePlugins },
			func() bool { return false }},
		{"autoStart", settingsItemAutoStart,
			func() bool { return d.autoStartAgent },
			func() bool { return false }},
		{"tmuxPersistence", settingsItemTmuxPersistence,
			func() bool { return d.tmuxPersistence },
			func() bool { return false }},
	}

	for _, c := range checkboxCases {
		before := c.read()
		clickItem(t, c.item)
		if c.read() == before {
			t.Errorf("%s: click did not toggle (still %v) — centering likely off", c.name, before)
		}
		if c.others() {
			t.Errorf("%s: a sibling checkbox changed state — click hit the wrong row", c.name)
		}
	}

	// Clicking "Close" closes the dialog.
	clickItem(t, settingsItemClose)
	if d.Visible() {
		t.Errorf("clicking Close did not hide the dialog — centering off")
	}
}
