package common

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ShowSoundPicker is sent when the user clicks "Notification sound" in settings.
type ShowSoundPicker struct{}

// SoundPickerResult is sent when the sound picker is closed.
type SoundPickerResult struct {
	Confirmed bool
	Sound     string
}

// SoundPreview is sent when the user navigates to a sound for live preview.
type SoundPreview struct {
	Sound string
}

// SoundPicker is a modal dialog for selecting notification sounds.
type SoundPicker struct {
	visible bool
	width   int
	height  int

	sounds        []string // "None" followed by sound names
	cursor        int
	originalSound string

	hitRegions        []soundHitRegion
	showKeymapHintsUI bool
}

type soundHitRegion struct {
	index  int
	region HitRegion
}

// NewSoundPicker creates a new sound picker with the current sound selected.
func NewSoundPicker(current string) *SoundPicker {
	sounds := []string{"None"}
	entries, _ := os.ReadDir("/System/Library/Sounds")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".aiff") {
			sounds = append(sounds, strings.TrimSuffix(name, filepath.Ext(name)))
		}
	}

	cursor := 0
	for i, s := range sounds {
		if s == current {
			cursor = i
			break
		}
	}

	return &SoundPicker{
		sounds:        sounds,
		cursor:        cursor,
		originalSound: current,
	}
}

func (p *SoundPicker) Show()                        { p.visible = true }
func (p *SoundPicker) Hide()                        { p.visible = false }
func (p *SoundPicker) Visible() bool                { return p.visible }
func (p *SoundPicker) SetSize(w, h int)             { p.width, p.height = w, h }
func (p *SoundPicker) SetShowKeymapHints(show bool) { p.showKeymapHintsUI = show }
func (p *SoundPicker) Cursor() *tea.Cursor          { return nil }

// Update handles input.
func (p *SoundPicker) Update(msg tea.Msg) (*SoundPicker, tea.Cmd) {
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
			return p, func() tea.Msg {
				return SoundPickerResult{Confirmed: false, Sound: p.originalSound}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			p.visible = false
			return p, func() tea.Msg {
				return SoundPickerResult{
					Confirmed: true,
					Sound:     p.selectedSound(),
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			return p.handleNext()

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			return p.handlePrev()
		}
	}

	return p, nil
}

func (p *SoundPicker) selectedSound() string {
	if p.cursor == 0 {
		return "" // "None"
	}
	return p.sounds[p.cursor]
}

func (p *SoundPicker) handleNext() (*SoundPicker, tea.Cmd) {
	p.cursor = (p.cursor + 1) % len(p.sounds)
	sound := p.selectedSound()
	if sound == "" {
		return p, nil
	}
	return p, func() tea.Msg { return SoundPreview{Sound: sound} }
}

func (p *SoundPicker) handlePrev() (*SoundPicker, tea.Cmd) {
	p.cursor--
	if p.cursor < 0 {
		p.cursor = len(p.sounds) - 1
	}
	sound := p.selectedSound()
	if sound == "" {
		return p, nil
	}
	return p, func() tea.Msg { return SoundPreview{Sound: sound} }
}

func (p *SoundPicker) handleClick(msg tea.MouseClickMsg) tea.Cmd {
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
			sound := p.selectedSound()
			if sound == "" {
				return nil
			}
			return func() tea.Msg { return SoundPreview{Sound: sound} }
		}
	}
	return nil
}

func (p *SoundPicker) View() string {
	if !p.visible {
		return ""
	}
	return p.build().View()
}

func (p *SoundPicker) dialogContentWidth() int {
	if p.width > 0 {
		return min(40, max(30, p.width-20))
	}
	return 35
}

func (p *SoundPicker) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(p.dialogContentWidth())
}

func (p *SoundPicker) build() *LineBuilder {
	b := NewLineBuilder(p.dialogStyle(), p.dialogContentWidth())
	p.hitRegions = p.hitRegions[:0]

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	b.Append("", title.Render("Notification Sound"))
	b.Append("", muted.Render("Play a sound when an agent needs input"))
	b.Blank()

	for i, s := range p.sounds {
		style, prefix := muted, "  "
		if i == p.cursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
			prefix = Icons.Cursor + " "
		}
		id := strconv.Itoa(i)
		b.Append(id, prefix+style.Render(s))
		if r, ok := b.RegionByID(id); ok {
			p.hitRegions = append(p.hitRegions, soundHitRegion{index: i, region: r})
		}
	}
	b.Blank()
	b.Append("", muted.Render("[Enter] Save • [Esc] Cancel"))

	if p.showKeymapHintsUI {
		b.Blank()
		b.Append("", muted.Render("↑/↓ navigate"))
	}
	return b
}
