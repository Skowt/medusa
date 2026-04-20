package common

import (
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ShowThemeEditor is sent when the user clicks "Change Theme" in settings.
type ShowThemeEditor struct{}

// ThemeResult is sent when the theme dialog is closed.
type ThemeResult struct {
	Confirmed bool
	Theme     ThemeID
}

// ThemeDialog is a modal dialog for selecting themes.
type ThemeDialog struct {
	visible bool
	width   int
	height  int

	// Theme selection state
	theme         ThemeID
	originalTheme ThemeID
	themeCursor   int
	themes        []Theme

	// For mouse hit detection
	hitRegions        []themeHitRegion
	showKeymapHintsUI bool
}

type themeHitRegion struct {
	index  int
	region HitRegion
}

// NewThemeDialog creates a new theme dialog with current values.
func NewThemeDialog(currentTheme ThemeID) *ThemeDialog {
	themes := AvailableThemes()
	themeCursor := 0
	for i, t := range themes {
		if t.ID == currentTheme {
			themeCursor = i
			break
		}
	}

	return &ThemeDialog{
		theme:         currentTheme,
		originalTheme: currentTheme,
		themes:        themes,
		themeCursor:   themeCursor,
	}
}

func (d *ThemeDialog) Show()                        { d.visible = true; d.originalTheme = d.theme }
func (d *ThemeDialog) Hide()                        { d.visible = false }
func (d *ThemeDialog) Visible() bool                { return d.visible }
func (d *ThemeDialog) SetSize(w, h int)             { d.width, d.height = w, h }
func (d *ThemeDialog) SetShowKeymapHints(show bool) { d.showKeymapHintsUI = show }
func (d *ThemeDialog) Cursor() *tea.Cursor          { return nil }

// SetTheme updates the dialog's selected theme (used when reopening dialog).
func (d *ThemeDialog) SetTheme(theme ThemeID) {
	d.theme = theme
	d.originalTheme = theme
	for i, t := range d.themes {
		if t.ID == theme {
			d.themeCursor = i
			break
		}
	}
}

// Update handles input.
func (d *ThemeDialog) Update(msg tea.Msg) (*ThemeDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return d, d.handleClick(msg)
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			d.theme = d.originalTheme
			d.visible = false
			return d, func() tea.Msg {
				return SafeBatch(
					func() tea.Msg { return ThemePreview{Theme: d.originalTheme} },
					func() tea.Msg { return ThemeResult{Confirmed: false} },
				)()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			d.visible = false
			return d, func() tea.Msg {
				return ThemeResult{
					Confirmed: true,
					Theme:     d.theme,
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			return d.handleNext()

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			return d.handlePrev()
		}
	}

	return d, nil
}

// handleNext cycles to the next theme.
func (d *ThemeDialog) handleNext() (*ThemeDialog, tea.Cmd) {
	d.themeCursor = (d.themeCursor + 1) % len(d.themes)
	d.theme = d.themes[d.themeCursor].ID
	return d, func() tea.Msg { return ThemePreview{Theme: d.theme} }
}

// handlePrev cycles to the previous theme.
func (d *ThemeDialog) handlePrev() (*ThemeDialog, tea.Cmd) {
	d.themeCursor--
	if d.themeCursor < 0 {
		d.themeCursor = len(d.themes) - 1
	}
	d.theme = d.themes[d.themeCursor].ID
	return d, func() tea.Msg { return ThemePreview{Theme: d.theme} }
}

func (d *ThemeDialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	b := d.build()
	dialogW, dialogH := b.Size()
	if dialogW == 0 || dialogH == 0 {
		return nil
	}
	dialogX, dialogY := centerOrigin(d.width, d.height, dialogW, dialogH)
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	contentOffsetX, contentOffsetY := b.ContentOffset()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range d.hitRegions {
		if hit.region.Contains(localX, localY) {
			d.themeCursor = hit.index
			d.theme = d.themes[d.themeCursor].ID
			return func() tea.Msg { return ThemePreview{Theme: d.theme} }
		}
	}
	return nil
}

func (d *ThemeDialog) View() string {
	if !d.visible {
		return ""
	}
	return d.build().View()
}

func (d *ThemeDialog) dialogContentWidth() int {
	if d.width > 0 {
		return min(40, max(30, d.width-20))
	}
	return 35
}

func (d *ThemeDialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(d.dialogContentWidth())
}

func (d *ThemeDialog) build() *LineBuilder {
	b := NewLineBuilder(d.dialogStyle(), d.dialogContentWidth())
	d.hitRegions = d.hitRegions[:0]

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	b.Append("", title.Render("Select Theme"))
	b.Blank()

	for i, t := range d.themes {
		style, prefix := muted, "  "
		if i == d.themeCursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
			prefix = Icons.Cursor + " "
		}
		id := strconv.Itoa(i)
		b.Append(id, prefix+style.Render(t.Name))
		if r, ok := b.RegionByID(id); ok {
			d.hitRegions = append(d.hitRegions, themeHitRegion{index: i, region: r})
		}
	}
	b.Blank()
	b.Append("", muted.Render("[Enter] Save • [Esc] Cancel"))

	if d.showKeymapHintsUI {
		b.Blank()
		b.Append("", muted.Render("↑/↓ navigate"))
	}

	return b
}

// centerOrigin returns the top-left corner of a (w, h) rectangle centered
// inside a (totalW, totalH) area, clamped to non-negative coordinates.
func centerOrigin(totalW, totalH, w, h int) (x, y int) {
	x, y = (totalW-w)/2, (totalH-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}
