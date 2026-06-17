package common

import (
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/ide"
)

// IDEInstallsDetected is sent after the action-bar IDE button triggers
// detection; it carries the installs to show in the picker and the workspace
// root to open once chosen.
type IDEInstallsDetected struct {
	Installs []ide.Install
	Root     string
}

// IDEPickerResult is sent when the IDE picker is closed.
type IDEPickerResult struct {
	Confirmed bool
	Install   ide.Install
	Root      string
}

// IDEPicker is a modal dialog for selecting which IDE install to open.
type IDEPicker struct {
	visible bool
	width   int
	height  int

	installs []ide.Install
	cursor   int
	root     string

	hitRegions        []ideHitRegion
	showKeymapHintsUI bool
}

type ideHitRegion struct {
	index  int
	region HitRegion
}

// NewIDEPicker creates a picker over installs, pre-selecting the install whose
// LaunchPath matches rememberedPath, or the first entry if none matches.
func NewIDEPicker(installs []ide.Install, rememberedPath, root string) *IDEPicker {
	cursor := 0
	if rememberedPath != "" {
		for i, ins := range installs {
			if ins.LaunchPath == rememberedPath {
				cursor = i
				break
			}
		}
	}
	return &IDEPicker{installs: installs, cursor: cursor, root: root}
}

func (p *IDEPicker) Show()                        { p.visible = true }
func (p *IDEPicker) Hide()                        { p.visible = false }
func (p *IDEPicker) Visible() bool                { return p.visible }
func (p *IDEPicker) SetSize(w, h int)             { p.width, p.height = w, h }
func (p *IDEPicker) SetShowKeymapHints(show bool) { p.showKeymapHintsUI = show }
func (p *IDEPicker) Cursor() *tea.Cursor          { return nil }

// Update handles input.
func (p *IDEPicker) Update(msg tea.Msg) (*IDEPicker, tea.Cmd) {
	if !p.visible {
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return p, p.handleClick(msg)
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			p.visible = false
			return p, func() tea.Msg { return IDEPickerResult{Confirmed: false} }

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			p.visible = false
			sel := p.installs[p.cursor]
			root := p.root
			return p, func() tea.Msg {
				return IDEPickerResult{Confirmed: true, Install: sel, Root: root}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			p.cursor = (p.cursor + 1) % len(p.installs)
			return p, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			p.cursor--
			if p.cursor < 0 {
				p.cursor = len(p.installs) - 1
			}
			return p, nil
		}
	}

	return p, nil
}

func (p *IDEPicker) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	b := p.build()
	dialogW, dialogH := b.Size()
	if dialogW == 0 || dialogH == 0 {
		return nil
	}
	dialogX, dialogY := centerOrigin(p.width, p.height, dialogW, dialogH)
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	contentOffsetX, contentOffsetY := b.ContentOffset()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range p.hitRegions {
		if hit.region.Contains(localX, localY) {
			p.cursor = hit.index
			return nil
		}
	}
	return nil
}

func (p *IDEPicker) View() string {
	if !p.visible {
		return ""
	}
	return p.build().View()
}

func (p *IDEPicker) dialogContentWidth() int {
	if p.width > 0 {
		return min(40, max(30, p.width-20))
	}
	return 35
}

func (p *IDEPicker) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(p.dialogContentWidth())
}

func (p *IDEPicker) build() *LineBuilder {
	b := NewLineBuilder(p.dialogStyle(), p.dialogContentWidth())
	p.hitRegions = p.hitRegions[:0]

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	b.Append("", title.Render("Open in IDE"))
	b.Append("", muted.Render("Choose which IDE to open this workspace in"))
	b.Blank()

	for i := range p.installs {
		style, prefix := muted, "  "
		if i == p.cursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
			prefix = Icons.Cursor + " "
		}
		id := strconv.Itoa(i)
		b.Append(id, prefix+style.Render(ide.DisplayLabel(p.installs, i)))
		if r, ok := b.RegionByID(id); ok {
			p.hitRegions = append(p.hitRegions, ideHitRegion{index: i, region: r})
		}
	}
	b.Blank()
	b.Append("", muted.Render("[Enter] Open • [Esc] Cancel"))

	if p.showKeymapHintsUI {
		b.Blank()
		b.Append("", muted.Render("↑/↓ navigate"))
	}
	return b
}
