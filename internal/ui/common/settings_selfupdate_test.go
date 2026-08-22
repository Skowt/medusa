package common

import (
	"regexp"
	"strings"
	"testing"
)

const testReinstallCmd = "curl -fsSL https://example.test/install.sh | sh"

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// flatten strips styling and box-drawing from a rendered dialog and collapses
// whitespace, so assertions survive the soft-wrapping the dialog applies to
// long lines like the reinstall command.
func flatten(view string) string {
	s := ansiRE.ReplaceAllString(view, "")
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune("│╭╮╰╯─", r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func newUpdateDialog(t *testing.T, blocked bool) *SettingsDialog {
	t.Helper()
	d := NewSettingsDialog(ThemeTokyoNight,
		false, false, false, false, false, "", false,
	)
	d.SetSize(120, 60)
	d.SetUpdateInfo("0.0.4", "0.0.5", true)
	d.SetSelfUpdateBlocked(blocked, testReinstallCmd)
	d.Show()
	return d
}

func renderDialog(t *testing.T, d *SettingsDialog) string {
	t.Helper()
	return flatten(d.View())
}

// When the binary sits somewhere unwritable, offering [Install update] would
// hand the user a button that always errors. Show the remedy instead.
func TestSettingsHidesInstallLinkWhenSelfUpdateBlocked(t *testing.T) {
	view := renderDialog(t, newUpdateDialog(t, true))

	if strings.Contains(view, "[Install update]") {
		t.Error("[Install update] must not be offered when the install dir is unwritable")
	}
	if !strings.Contains(view, "Cannot update in place") {
		t.Error("expected an explanation that the update cannot be installed")
	}
	if !strings.Contains(view, testReinstallCmd) {
		t.Errorf("expected the reinstall command in the view, got:\n%s", view)
	}
	// The changelog stays reachable — being unable to install is no reason to
	// hide what changed.
	if !strings.Contains(view, "[View changes]") {
		t.Error("[View changes] should remain available when blocked")
	}
}

func TestSettingsShowsInstallLinkWhenWritable(t *testing.T) {
	view := renderDialog(t, newUpdateDialog(t, false))

	if !strings.Contains(view, "[Install update]") {
		t.Error("[Install update] should be offered on a writable install")
	}
	if strings.Contains(view, "Cannot update in place") {
		t.Error("no blocked hint expected on a writable install")
	}
}

// Selecting the (unrendered) upgrade item must not fire an upgrade request.
func TestSettingsUpgradeSelectIsInertWhenBlocked(t *testing.T) {
	d := newUpdateDialog(t, true)
	d.focusedItem = settingsItemUpgrade

	_, cmd := d.handleSelect()
	if cmd != nil {
		t.Error("selecting [Install update] while blocked must not trigger an upgrade")
	}
	if !d.Visible() {
		t.Error("dialog should stay open when the inert upgrade item is selected")
	}
}

func TestSettingsUpgradeSelectFiresWhenWritable(t *testing.T) {
	d := newUpdateDialog(t, false)
	d.focusedItem = settingsItemUpgrade

	_, cmd := d.handleSelect()
	if cmd == nil {
		t.Fatal("selecting [Install update] should trigger an upgrade")
	}
	if _, ok := cmd().(TriggerUpgradeRequest); !ok {
		t.Error("expected a TriggerUpgradeRequest")
	}
}

// Keyboard navigation must not park focus on a link that isn't drawn.
func TestSettingsNavigationSkipsBlockedUpgradeItem(t *testing.T) {
	d := newUpdateDialog(t, true)

	d.focusedItem = settingsItemReleases
	d.handleNextSection()
	if d.focusedItem == settingsItemUpgrade {
		t.Error("forward navigation should skip the blocked [Install update] item")
	}

	d.focusedItem = settingsItemSave
	d.handlePrevSection()
	if d.focusedItem == settingsItemUpgrade {
		t.Error("backward navigation should skip the blocked [Install update] item")
	}
	if d.focusedItem != settingsItemReleases {
		t.Errorf("backward navigation should land on [View changes], got %v", d.focusedItem)
	}
}

// With an update available and a writable dir, both About links stay reachable.
func TestSettingsNavigationReachesUpgradeWhenWritable(t *testing.T) {
	d := newUpdateDialog(t, false)

	d.focusedItem = settingsItemReleases
	d.handleNextSection()
	if d.focusedItem != settingsItemUpgrade {
		t.Errorf("forward navigation should reach [Install update], got %v", d.focusedItem)
	}
}

// A blocked install with no update pending still tells the user their install
// is stuck — that is the only place the message is discoverable on demand.
func TestSettingsShowsBlockedHintWithoutPendingUpdate(t *testing.T) {
	d := NewSettingsDialog(ThemeTokyoNight,
		false, false, false, false, false, "", false,
	)
	d.SetSize(120, 60)
	d.SetUpdateInfo("0.0.4", "", false)
	d.SetSelfUpdateBlocked(true, testReinstallCmd)
	d.Show()

	view := renderDialog(t, d)
	if !strings.Contains(view, "No new updates") {
		t.Error("expected the no-updates line")
	}
	if !strings.Contains(view, testReinstallCmd) {
		t.Error("expected the reinstall command even with no pending update")
	}
}
