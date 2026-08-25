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

// IDEPickerResult is sent when the IDE picker is closed. DontAskAgain carries
// the checkbox state so a confirmed pick can also turn the prompt off, and a
// cancel leaves the stored preference untouched.
type IDEPickerResult struct {
	Confirmed    bool
	Install      ide.Install
	Root         string
	DontAskAgain bool
}

// IDEPicker is a modal dialog for selecting which IDE install to open.
type IDEPicker struct {
	visible bool
	width   int
	height  int

	installs []ide.Install
	cursor   int
	root     string
	dontAsk  bool

	hitRegions        []ideHitRegion
	showKeymapHintsUI bool
}

type ideHitRegion struct {
	index  int
	region HitRegion
}

// NewIDEPicker creates a picker over installs, pre-selecting the install whose
// LaunchPath matches rememberedPath, or the first entry if none matches.
// dontAsk seeds the "don't ask again" checkbox: the picker still opens with it
// on when the remembered install has gone missing, so the preference is shown
// as it stands rather than silently reset.
func NewIDEPicker(installs []ide.Install, rememberedPath, root string, dontAsk bool) *IDEPicker {
	cursor := 0
	if rememberedPath != "" {
		for i, ins := range installs {
			if ins.LaunchPath == rememberedPath {
				cursor = i
				break
			}
		}
	}
	return &IDEPicker{installs: installs, cursor: cursor, root: root, dontAsk: dontAsk}
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
			return p, p.selectRow()

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			p.cursor = (p.cursor + 1) % p.rowCount()
			return p, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			p.cursor--
			if p.cursor < 0 {
				p.cursor = p.rowCount() - 1
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

	// Clicking a row both selects and acts on it — a click is the mouse
	// equivalent of moving the cursor there and pressing enter.
	for _, hit := range p.hitRegions {
		if hit.region.Contains(localX, localY) {
			p.cursor = hit.index
			return p.selectRow()
		}
	}
	return nil
}

// checkboxIndex is the focus position of the "don't ask again" row, which sits
// after the last install.
func (p *IDEPicker) checkboxIndex() int { return len(p.installs) }

// rowCount is the number of focusable rows: every install plus the checkbox.
func (p *IDEPicker) rowCount() int { return len(p.installs) + 1 }

// selectRow acts on the focused row: the checkbox toggles, an install opens.
func (p *IDEPicker) selectRow() tea.Cmd {
	if p.cursor == p.checkboxIndex() {
		p.dontAsk = !p.dontAsk
		return nil
	}
	return p.confirm()
}

// confirm closes the picker and reports the install under the cursor.
func (p *IDEPicker) confirm() tea.Cmd {
	if p.cursor < 0 || p.cursor >= len(p.installs) {
		return nil
	}
	p.visible = false
	sel := p.installs[p.cursor]
	root := p.root
	dontAsk := p.dontAsk
	return func() tea.Msg {
		return IDEPickerResult{Confirmed: true, Install: sel, Root: root, DontAskAgain: dontAsk}
	}
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
	p.appendDontAsk(b, muted)

	b.Blank()
	// The hint follows the focus: enter opens an install but only toggles the
	// checkbox, and a hint that says "Open" there reads as a dead key.
	action := "[Enter] Open • [Esc] Cancel"
	if p.cursor == p.checkboxIndex() {
		action = "[Enter] Toggle • [Esc] Cancel"
	}
	b.Append("", muted.Render(action))

	if p.showKeymapHintsUI {
		b.Blank()
		b.Append("", muted.Render("↑/↓ navigate"))
	}
	return b
}

// appendDontAsk renders the "don't ask again" checkbox as one more focusable
// row after the installs, plus the note that Settings can turn the prompt back
// on — a toggle that reads as one-way is one users won't touch.
func (p *IDEPicker) appendDontAsk(b *LineBuilder, muted lipgloss.Style) {
	box := "[ ]"
	if p.dontAsk {
		box = "[" + Icons.Clean + "]"
	}
	style := lipgloss.NewStyle().Foreground(ColorForeground)
	if p.cursor == p.checkboxIndex() {
		style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	}
	const id = "dontask"
	b.Append(id, style.Render(box+" Don't ask again"))
	if r, ok := b.RegionByID(id); ok {
		p.hitRegions = append(p.hitRegions, ideHitRegion{index: p.checkboxIndex(), region: r})
	}
	b.Append("", muted.Render("    Reversible in Settings"))
}
