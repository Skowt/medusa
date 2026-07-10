package common

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SettingsResult is sent when the settings dialog is closed.
type SettingsResult struct {
	Confirmed          bool
	Theme              ThemeID
	ShowKeymapHints    bool
	HideSidebar        bool
	HideTerminal       bool
	AutoStartAgent     bool
	SyncProfilePlugins bool
	GlobalPermissions  bool
	CompoundApprove    bool
	NotificationSound  string
	TmuxPersistence    bool
}

// ShowPermissionsEditor is sent when the user clicks "Edit Global Allow/Deny List".
type ShowPermissionsEditor struct{}

// TriggerUpgradeRequest is sent when the user clicks "Install update" in settings.
// The app handler translates this into messages.TriggerUpgrade{}.
type TriggerUpgradeRequest struct{}

// ThemePreview is sent when user navigates through themes for live preview.
type ThemePreview struct {
	Theme ThemeID
}

type settingsItem int

const (
	settingsItemKeymap settingsItem = iota
	settingsItemHideSidebar
	settingsItemHideTerminal
	settingsItemSyncPlugins // Shared Config section
	settingsItemGlobalPerms
	settingsItemEditPermissions
	settingsItemCompoundApprove // Agents section
	settingsItemNotificationSound
	settingsItemAutoStart // Tmux section
	settingsItemTmuxPersistence
	settingsItemManageProfiles
	settingsItemEditTheme
	settingsItemReleases // About section - only selectable when updateAvailable
	settingsItemUpgrade  // About section - only selectable when updateAvailable
	settingsItemSave
	settingsItemClose
)

// medusaReleasesURL is the GitHub releases page — linked from the Settings
// About section when an update is available so users can review the changelog.
const medusaReleasesURL = "https://github.com/Skowt/medusa/releases/"

// SettingsDialog is a modal dialog for application settings.
type SettingsDialog struct {
	visible bool
	width   int
	height  int

	// Settings values
	theme              ThemeID
	showKeymapHints    bool
	hideSidebar        bool
	hideTerminal       bool
	autoStartAgent     bool
	syncProfilePlugins bool
	globalPerms        bool
	compoundApprove    bool
	notificationSound  string
	tmuxPersistence    bool

	// UI state
	focusedItem settingsItem

	// For mouse hit detection
	hitRegions        []settingsHitRegion
	showKeymapHintsUI bool

	// Update state
	currentVersion  string
	latestVersion   string
	updateAvailable bool
	// selfUpdateBlocked is set when medusa's binary lives in a directory the
	// user cannot write, so an in-place upgrade would fail. The reinstall
	// command is shown instead of the [Install update] link.
	selfUpdateBlocked bool
	reinstallCommand  string
}

type settingsHitRegion struct {
	item   settingsItem
	index  int
	region HitRegion
}

// NewSettingsDialog creates a new settings dialog with current values.
func NewSettingsDialog(currentTheme ThemeID, showKeymapHints, hideSidebar, hideTerminal, autoStartAgent, syncProfilePlugins, globalPerms, compoundApprove bool, notificationSound string, tmuxPersistence bool) *SettingsDialog {
	return &SettingsDialog{
		theme:              currentTheme,
		showKeymapHints:    showKeymapHints,
		hideSidebar:        hideSidebar,
		hideTerminal:       hideTerminal,
		autoStartAgent:     autoStartAgent,
		syncProfilePlugins: syncProfilePlugins,
		globalPerms:        globalPerms,
		compoundApprove:    compoundApprove,
		notificationSound:  notificationSound,
		tmuxPersistence:    tmuxPersistence,
		focusedItem:        settingsItemKeymap,
	}
}

func (s *SettingsDialog) Show()                             { s.visible = true }
func (s *SettingsDialog) Hide()                             { s.visible = false }
func (s *SettingsDialog) Visible() bool                     { return s.visible }
func (s *SettingsDialog) SetSize(w, h int)                  { s.width, s.height = w, h }
func (s *SettingsDialog) SetShowKeymapHints(show bool)      { s.showKeymapHintsUI = show }
func (s *SettingsDialog) Cursor() *tea.Cursor               { return nil }
func (s *SettingsDialog) SetTheme(theme ThemeID)            { s.theme = theme }
func (s *SettingsDialog) SetNotificationSound(sound string) { s.notificationSound = sound }

// SetUpdateInfo sets version information for the updates section.
func (s *SettingsDialog) SetUpdateInfo(current, latest string, available bool) {
	s.currentVersion = current
	s.latestVersion = latest
	s.updateAvailable = available
}

// SetSelfUpdateBlocked reports that medusa cannot install over its own binary.
// command is the reinstall command to show the user.
func (s *SettingsDialog) SetSelfUpdateBlocked(blocked bool, command string) {
	s.selfUpdateBlocked = blocked
	s.reinstallCommand = command
}

// canInstallUpdate reports whether the [Install update] link should be offered.
func (s *SettingsDialog) canInstallUpdate() bool {
	return s.updateAvailable && !s.selfUpdateBlocked
}

// Update handles input.
func (s *SettingsDialog) Update(msg tea.Msg) (*SettingsDialog, tea.Cmd) {
	if !s.visible {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return s, s.handleClick(msg)
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			s.visible = false
			return s, func() tea.Msg { return SettingsResult{Confirmed: false} }

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			return s.handleSelect()

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			return s.handleNextSection()

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
			return s.handlePrevSection()

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			return s.handleNext()

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			return s.handlePrev()
		}
	}

	return s, nil
}

func (s *SettingsDialog) handleSelect() (*SettingsDialog, tea.Cmd) {
	switch s.focusedItem {
	case settingsItemEditTheme:
		s.visible = false
		return s, func() tea.Msg { return ShowThemeEditor{} }

	case settingsItemKeymap:
		s.showKeymapHints = !s.showKeymapHints
		return s, nil

	case settingsItemHideSidebar:
		s.hideSidebar = !s.hideSidebar
		return s, nil

	case settingsItemHideTerminal:
		s.hideTerminal = !s.hideTerminal
		return s, nil

	case settingsItemNotificationSound:
		s.visible = false
		return s, func() tea.Msg { return ShowSoundPicker{} }

	case settingsItemAutoStart:
		s.autoStartAgent = !s.autoStartAgent
		return s, nil

	case settingsItemSyncPlugins:
		s.syncProfilePlugins = !s.syncProfilePlugins
		return s, nil

	case settingsItemManageProfiles:
		s.visible = false
		return s, func() tea.Msg { return ShowProfileManager{} }

	case settingsItemGlobalPerms:
		s.globalPerms = !s.globalPerms
		return s, nil

	case settingsItemEditPermissions:
		if s.globalPerms {
			s.visible = false
			return s, func() tea.Msg { return ShowPermissionsEditor{} }
		}
		return s, nil

	case settingsItemCompoundApprove:
		s.compoundApprove = !s.compoundApprove
		return s, nil

	case settingsItemReleases:
		if !s.updateAvailable {
			return s, nil
		}
		return s, openURL(medusaReleasesURL)

	case settingsItemUpgrade:
		if !s.canInstallUpdate() {
			return s, nil
		}
		s.visible = false
		return s, func() tea.Msg { return TriggerUpgradeRequest{} }

	case settingsItemTmuxPersistence:
		s.tmuxPersistence = !s.tmuxPersistence
		return s, nil

	case settingsItemSave:
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{
				Confirmed:          true,
				Theme:              s.theme,
				ShowKeymapHints:    s.showKeymapHints,
				HideSidebar:        s.hideSidebar,
				HideTerminal:       s.hideTerminal,
				AutoStartAgent:     s.autoStartAgent,
				SyncProfilePlugins: s.syncProfilePlugins,
				GlobalPermissions:  s.globalPerms,
				CompoundApprove:    s.compoundApprove,
				NotificationSound:  s.notificationSound,
				TmuxPersistence:    s.tmuxPersistence,
			}
		}

	case settingsItemClose:
		s.visible = false
		return s, func() tea.Msg { return SettingsResult{Confirmed: false} }
	}
	return s, nil
}

// handleNextSection moves focus to the next section (Tab key).
func (s *SettingsDialog) handleNextSection() (*SettingsDialog, tea.Cmd) {
	s.focusedItem++
	s.skipDisabledForward()
	if s.focusedItem > settingsItemClose {
		s.focusedItem = settingsItemKeymap
	}
	return s, nil
}

// handlePrevSection moves focus to the previous section (Shift+Tab key).
func (s *SettingsDialog) handlePrevSection() (*SettingsDialog, tea.Cmd) {
	s.focusedItem--
	s.skipDisabledBackward()
	if s.focusedItem < 0 {
		s.focusedItem = settingsItemClose
	}
	return s, nil
}

func (s *SettingsDialog) skipDisabledForward() {
	// Skip edit permissions when global perms is off
	if !s.globalPerms && s.focusedItem == settingsItemEditPermissions {
		s.focusedItem = settingsItemNotificationSound
	}
	// Skip [View changes] when no update is available, then [Install update]
	// when it is absent too — a blocked install keeps the changelog reachable.
	if !s.updateAvailable && s.focusedItem == settingsItemReleases {
		s.focusedItem = settingsItemUpgrade
	}
	if !s.canInstallUpdate() && s.focusedItem == settingsItemUpgrade {
		s.focusedItem = settingsItemSave
	}
}

func (s *SettingsDialog) skipDisabledBackward() {
	// Skip edit permissions when global perms is off
	if !s.globalPerms && s.focusedItem == settingsItemEditPermissions {
		s.focusedItem = settingsItemGlobalPerms
	}
	// Mirror of skipDisabledForward, walking the About items in reverse.
	if !s.canInstallUpdate() && s.focusedItem == settingsItemUpgrade {
		s.focusedItem = settingsItemReleases
	}
	if !s.updateAvailable && s.focusedItem == settingsItemReleases {
		s.focusedItem = settingsItemEditTheme
	}
	// Wrap around from before first item to last
	if s.focusedItem < 0 {
		s.focusedItem = settingsItemClose
	}
}

// handleNext moves to next item (down/j keys).
func (s *SettingsDialog) handleNext() (*SettingsDialog, tea.Cmd) {
	return s.handleNextSection()
}

// handlePrev moves to previous item (up/k keys).
func (s *SettingsDialog) handlePrev() (*SettingsDialog, tea.Cmd) {
	return s.handlePrevSection()
}

func (s *SettingsDialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	b := s.build()
	dialogW, dialogH := b.Size()
	if dialogW == 0 || dialogH == 0 {
		return nil
	}
	dialogX, dialogY := centerOrigin(s.width, s.height, dialogW, dialogH)
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	contentOffsetX, contentOffsetY := b.ContentOffset()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range s.hitRegions {
		if hit.region.Contains(localX, localY) {
			s.focusedItem = hit.item
			_, cmd := s.handleSelect()
			return cmd
		}
	}
	return nil
}

func (s *SettingsDialog) View() string {
	if !s.visible {
		return ""
	}
	return s.build().View()
}

func (s *SettingsDialog) dialogContentWidth() int {
	if s.width > 0 {
		return min(50, max(35, s.width-20))
	}
	return 40
}

func (s *SettingsDialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(s.dialogContentWidth())
}
