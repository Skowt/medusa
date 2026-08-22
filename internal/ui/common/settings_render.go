package common

import (
	"strconv"

	"charm.land/lipgloss/v2"
)

// build constructs the dialog's rendered rows and hit regions. Calling it
// populates s.hitRegions as a side effect so handleClick and View share a
// single source of truth for geometry.
func (s *SettingsDialog) build() *LineBuilder {
	b := NewLineBuilder(s.dialogStyle(), s.dialogContentWidth())
	s.hitRegions = s.hitRegions[:0]

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	label := lipgloss.NewStyle().Foreground(ColorMuted)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	b.Append("", title.Render("Settings"))
	b.Blank()

	// ── General ──────────────────────────────────────────────
	s.appendCheckbox(b, settingsItemKeymap, s.showKeymapHints, "Show keymap hints")
	s.appendCheckbox(b, settingsItemHideSidebar, s.hideSidebar, "Hide sidebar")
	s.appendCheckbox(b, settingsItemHideTerminal, s.hideTerminal, "Hide terminal")
	b.Blank()

	// ── Shared Config ────────────────────────────────────────
	b.Append("", label.Render("Shared Config"))
	s.appendCheckbox(b, settingsItemSyncPlugins, s.syncProfilePlugins, "Sync plugins & skills across profiles")
	b.Blank()

	// ── Agents ───────────────────────────────────────────────
	b.Append("", label.Render("Agents"))
	soundLabel := "None"
	if s.notificationSound != "" {
		soundLabel = s.notificationSound
	}
	s.appendLink(b, settingsItemNotificationSound, muted, "[Notification sound: "+soundLabel+"]")
	b.Blank()

	// ── Tmux ─────────────────────────────────────────────────
	b.Append("", label.Render("Tmux"))
	s.appendCheckbox(b, settingsItemAutoStart, s.autoStartAgent, "Auto start agent in new worktrees")
	s.appendCheckbox(b, settingsItemTmuxPersistence, s.tmuxPersistence, "Keep sessions alive across restarts")
	b.Blank()

	// ── Other ────────────────────────────────────────────────
	s.appendLink(b, settingsItemManageProfiles, muted, "[Manage Profiles]")

	currentTheme := GetTheme(s.theme)
	s.appendLink(b, settingsItemEditTheme, muted, "[Change Theme: "+currentTheme.Name+"]")
	b.Blank()

	// ── About ────────────────────────────────────────────────
	b.Append("", label.Render("About"))
	versionText := s.currentVersion
	if versionText == "" {
		versionText = "dev"
	}
	b.Append("", muted.Render("Version: "+versionText))
	if s.updateAvailable {
		b.Append("", muted.Render("Update available → "+s.latestVersion))
		s.appendLink(b, settingsItemReleases, muted, "[View changes]")
		if s.canInstallUpdate() {
			s.appendLinkBold(b, settingsItemUpgrade, "[Install update]")
		}
	} else {
		b.Append("", muted.Render("No new updates"))
	}
	if s.selfUpdateBlocked {
		b.Append("", muted.Render("Cannot update in place — medusa is installed"))
		b.Append("", muted.Render("somewhere you can't write. Reinstall with:"))
		b.Append("", muted.Render("  "+s.reinstallCommand))
	}
	b.Blank()

	s.appendLinkBold(b, settingsItemSave, "[Save settings]")
	b.Blank()

	s.appendLink(b, settingsItemClose, muted, "[Esc] Close")

	if s.showKeymapHintsUI {
		b.Blank()
		b.Append("", muted.Render("↑/↓ navigate • Enter select/save"))
	}

	return b
}

// appendCheckbox renders a "[ ] label" or "[✓] label" line and records a hit
// region spanning the row so clicks anywhere on the row toggle the setting.
func (s *SettingsDialog) appendCheckbox(b *LineBuilder, item settingsItem, checked bool, label string) {
	box := "[ ]"
	if checked {
		box = "[" + Icons.Clean + "]"
	}
	style := lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == item {
		style = style.Foreground(ColorPrimary)
	}
	id := itemID(item)
	b.Append(id, style.Render(box+" "+label))
	s.recordHit(item, b)
}

// appendLink renders a clickable "[Label]" line with the given base style,
// promoting it to ColorPrimary when focused.
func (s *SettingsDialog) appendLink(b *LineBuilder, item settingsItem, base lipgloss.Style, text string) {
	style := base
	if s.focusedItem == item {
		style = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	id := itemID(item)
	b.Append(id, style.Render(text))
	s.recordHit(item, b)
}

// appendLinkBold renders a clickable line that is bold when focused (used for
// the primary action buttons like Save and Install update).
func (s *SettingsDialog) appendLinkBold(b *LineBuilder, item settingsItem, text string) {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	if s.focusedItem == item {
		style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	}
	id := itemID(item)
	b.Append(id, style.Render(text))
	s.recordHit(item, b)
}

// recordHit copies the most recently appended region from b into the dialog's
// typed hit table so handleClick can resolve it back to a settingsItem.
func (s *SettingsDialog) recordHit(item settingsItem, b *LineBuilder) {
	r, ok := b.RegionByID(itemID(item))
	if !ok {
		return
	}
	s.hitRegions = append(s.hitRegions, settingsHitRegion{
		item:   item,
		index:  -1,
		region: r,
	})
}

func itemID(item settingsItem) string {
	return strconv.Itoa(int(item))
}
