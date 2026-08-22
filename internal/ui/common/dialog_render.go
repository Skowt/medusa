package common

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// HitRegion IDs registered by the Dialog builder. handleClick dispatches by
// these IDs rather than iterating regions in declaration order, so adding a
// new region can't accidentally shadow an existing one.
const (
	dialogIDInput       = "input"
	dialogIDFilterInput = "filter-input"
	dialogIDCheckbox1   = "checkbox-1"
	dialogIDCheckbox2   = "checkbox-2"
	dialogIDCheckbox3   = "checkbox-3"
	dialogIDSelectField = "select"
	dialogIDSelectLeft  = "select-left"
	dialogIDSelectRight = "select-right"
	dialogIDOK          = "ok"
	dialogIDCancel      = "cancel"
	dialogIDOptPrefix   = "option-" // followed by index: option-0, option-1, …
)

func viewDimensions(view string) (width, height int) {
	lines := strings.Split(view, "\n")
	height = len(lines)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width, height
}

// View renders the dialog through the LineBuilder pipeline so click regions
// always align with the actually-drawn rows (lipgloss soft-wrapping was
// previously drifting the row count out of sync with hit regions).
func (d *Dialog) View() string {
	if !d.visible {
		return ""
	}
	return d.build().View()
}

// Cursor returns the cursor position relative to the dialog view. We look
// up the input row's region in the builder rather than recomputing prefix
// height — the builder is the single source of truth for layout.
func (d *Dialog) Cursor() *tea.Cursor {
	if !d.visible {
		return nil
	}

	var inputID string
	var c *tea.Cursor
	switch d.dtype {
	case DialogInput:
		if d.inputHidden || d.input.VirtualCursor() || !d.input.Focused() {
			return nil
		}
		inputID = dialogIDInput
		c = d.input.Cursor()
	case DialogSelect:
		if !d.filterEnabled || d.filterInput.VirtualCursor() || !d.filterInput.Focused() {
			return nil
		}
		inputID = dialogIDFilterInput
		c = d.filterInput.Cursor()
	default:
		return nil
	}
	if c == nil {
		return nil
	}

	b := d.build()
	region, ok := b.RegionByID(inputID)
	if !ok {
		return nil
	}
	contentX, contentY := b.ContentOffset()
	c.X += contentX
	c.Y += contentY + region.Y
	return c
}

func (d *Dialog) dialogContentWidth() int {
	width := 50
	if d.width > 0 {
		width = min(80, max(50, d.width-10))
	}
	return width
}

func (d *Dialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(d.dialogContentWidth())
}

// build constructs the full dialog as a LineBuilder. Render output, click
// dispatch, and Cursor positioning all derive from this single source of
// truth, so they cannot disagree about where things are drawn.
func (d *Dialog) build() *LineBuilder {
	b := NewLineBuilder(d.dialogStyle(), d.dialogContentWidth())

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	b.Append("", titleStyle.Render(d.title))

	switch d.dtype {
	case DialogInput:
		d.buildInput(b)
	case DialogConfirm:
		d.buildConfirm(b)
	case DialogSelect:
		d.buildSelect(b)
	}

	if d.showKeymapHints {
		helpStyle := lipgloss.NewStyle().Foreground(ColorMuted).MarginTop(1)
		b.Blank()
		b.Append("", helpStyle.Render(d.helpText()))
	}

	return b
}

func (d *Dialog) buildInput(b *LineBuilder) {
	if d.message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		b.Append("", msgStyle.Render(d.message))
		b.Blank()
	}
	if !d.inputHidden {
		b.Append(dialogIDInput, d.input.View())
		if d.validationErr != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorError)
			b.Append("", errStyle.Render(d.validationErr))
		}
	}
	// Inline selects first — the primary controls of the dialog.
	for slot := range d.sel {
		if !d.sel[slot].present() {
			continue
		}
		if slot > 0 || !d.inputHidden || d.message == "" {
			b.Blank()
		}
		d.appendSelectField(b, slot)
	}
	// Then checkboxes.
	if d.checkboxLabel != "" {
		if d.hasSelect() || !d.inputHidden || d.message == "" {
			b.Blank()
		}
		d.appendCheckbox(b, dialogIDCheckbox1, d.checkboxLabel, d.checkboxValue, d.checkboxFocused, false, d.checkboxDesc)
	}
	if d.checkbox2Label != "" {
		if d.checkboxLabel == "" {
			b.Blank()
		}
		d.appendCheckbox(b, dialogIDCheckbox2, d.checkbox2Label, d.checkbox2Value, d.checkbox2Focused, d.checkbox2Disabled(), d.checkbox2Desc)
	}
	// Checkbox 3 is never nested under 1/2, so it always gets a separating blank.
	if d.checkbox3Label != "" {
		b.Blank()
		d.appendCheckbox(b, dialogIDCheckbox3, d.checkbox3Label, d.checkbox3Value, d.checkbox3Focused, false, d.checkbox3Desc)
	}
	b.Blank()
	d.appendInputButtons(b)
}

func (d *Dialog) buildConfirm(b *LineBuilder) {
	b.Append("", d.message)
	b.Blank()
	d.appendOptions(b)
}

func (d *Dialog) buildSelect(b *LineBuilder) {
	if d.message != "" {
		b.Append("", d.message)
		b.Blank()
	}
	if d.filterEnabled {
		b.Append(dialogIDFilterInput, d.filterInput.View())
		b.Blank()
	}
	d.appendOptions(b)
}

// appendCheckbox renders one checkbox row + an optional indented description.
// The description is pre-wrapped to known widths and appended via AppendRaw
// so the builder's row count tracks reality (lipgloss soft-wrapping caused
// drift previously).
func (d *Dialog) appendCheckbox(b *LineBuilder, id, label string, value, focused, disabled bool, desc string) {
	box := "[ ]"
	if value {
		box = "[" + Icons.Clean + "]"
	}
	style := lipgloss.NewStyle().Foreground(ColorForeground)
	switch {
	case disabled:
		style = style.Foreground(ColorMuted)
	case focused:
		style = style.Foreground(ColorPrimary)
	}
	b.Append(id, style.Render(box+" "+label))
	if desc != "" {
		descStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		for _, line := range wordWrap(desc, b.ContentWidth()-4) {
			b.AppendRaw("", "    "+descStyle.Render(line))
		}
	}
}

// appendSelectField renders the inline cycler ("Label:  < Current >") plus
// the current option's description. The full row is registered as
// dialogIDSelectField; the chevrons get inline regions so clicks can
// distinguish "cycle back" vs "cycle forward".
func (d *Dialog) appendSelectField(b *LineBuilder, slot int) {
	field := &d.sel[slot]
	labelStyle := lipgloss.NewStyle().Foreground(ColorForeground)
	if field.disabled {
		labelStyle = labelStyle.Foreground(ColorMuted)
	} else if field.focused {
		labelStyle = labelStyle.Foreground(ColorPrimary)
	}
	arrowStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	valueStyle := lipgloss.NewStyle().Foreground(ColorForeground).Bold(field.focused)
	if field.disabled {
		arrowStyle = arrowStyle.Foreground(ColorMuted)
		valueStyle = valueStyle.Foreground(ColorMuted).Bold(false)
	}

	current := field.current()
	left := arrowStyle.Render("<")
	right := arrowStyle.Render(">")
	value := valueStyle.Render(current.Label)
	row := labelStyle.Render(field.label) + "  " + left + " " + value + " " + right

	rowY := b.CurrentRow()
	b.Append(selectRegionID(dialogIDSelectField, slot), row)

	labelW := lipgloss.Width(field.label) + 2 // label + "  "
	leftW := lipgloss.Width(left)
	if !field.disabled {
		b.AddRegion(selectRegionID(dialogIDSelectLeft, slot), labelW, rowY, leftW, 1)
		rightX := labelW + leftW + 1 + lipgloss.Width(value) + 1
		b.AddRegion(selectRegionID(dialogIDSelectRight, slot), rightX, rowY, lipgloss.Width(right), 1)
	}

	if current.Description != "" {
		descStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		for _, line := range wordWrap(current.Description, b.ContentWidth()-4) {
			b.AppendRaw("", "    "+descStyle.Render(line))
		}
	}
}

// appendInputButtons renders the OK / Cancel button row for a DialogInput
// with two inline regions — clicking the OK column submits, clicking the
// Cancel column dismisses.
func (d *Dialog) appendInputButtons(b *LineBuilder) {
	bracketStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	textStyle := lipgloss.NewStyle().Foreground(ColorForeground)
	normalStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	ok := bracketStyle.Render("[") + textStyle.Render(" OK ") + bracketStyle.Render("]")
	cancel := normalStyle.Render("[ Cancel ]")

	const gap = 2
	okWidth := lipgloss.Width(ok)
	cancelX := okWidth + gap
	cancelWidth := min(lipgloss.Width(cancel), max(0, b.ContentWidth()-cancelX))

	rowY := b.CurrentRow()
	b.Append("", ok+"  "+cancel)
	b.AddRegion(dialogIDOK, 0, rowY, okWidth+gap, 1)
	b.AddRegion(dialogIDCancel, cancelX, rowY, cancelWidth, 1)
}

// appendOptions renders DialogConfirm/DialogSelect options either vertically
// (one per row, registered as option-N) or horizontally (single row with
// inline regions per option), followed by the focused option's hint if it has
// one.
func (d *Dialog) appendOptions(b *LineBuilder) {
	if d.verticalLayout || (d.dtype == DialogSelect && d.filterEnabled) {
		d.appendVerticalOptions(b)
	} else {
		d.appendHorizontalOptions(b)
	}
	d.appendFocusedOptionHint(b)
}

// appendFocusedOptionHint renders the hint for the currently focused option as a
// dimmed, word-wrapped block below the option row. Pre-wrapped and appended via
// AppendRaw so the builder's row count matches what's drawn — lipgloss
// soft-wrapping would drift it out of sync with the click regions.
func (d *Dialog) appendFocusedOptionHint(b *LineBuilder) {
	hint := d.optionHint(d.focusedOptionIndex())
	if hint == "" {
		return
	}
	hintStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	b.Blank()
	for _, line := range wordWrap(hint, b.ContentWidth()) {
		b.AppendRaw("", hintStyle.Render(line))
	}
}

// focusedOptionIndex maps d.cursor back to an index into d.options. Under a
// fuzzy filter the cursor indexes the visible subset, not the full list.
func (d *Dialog) focusedOptionIndex() int {
	indices := d.visibleOptionIndices()
	if d.cursor < 0 || d.cursor >= len(indices) {
		return -1
	}
	return indices[d.cursor]
}

func (d *Dialog) appendVerticalOptions(b *LineBuilder) {
	indices := d.visibleOptionIndices()
	for cursorIdx, originalIdx := range indices {
		opt := d.options[originalIdx]
		cursor := Icons.CursorEmpty + " "
		nameStyle := lipgloss.NewStyle().Foreground(ColorForeground)
		if cursorIdx == d.cursor {
			cursor = Icons.Cursor + " "
			nameStyle = nameStyle.Bold(true)
		}
		id := dialogIDOptPrefix + itoa(originalIdx)
		b.Append(id, cursor+nameStyle.Render(opt))
	}
}

func (d *Dialog) appendHorizontalOptions(b *LineBuilder) {
	bracketStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	selectedTextStyle := lipgloss.NewStyle().Foreground(ColorForeground)
	normalStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	const gap = 2
	rowY := b.CurrentRow()
	var sb strings.Builder
	x := 0
	for i, opt := range d.options {
		var rendered string
		if i == d.cursor {
			rendered = bracketStyle.Render("[") + selectedTextStyle.Render(" "+opt+" ") + bracketStyle.Render("]")
		} else {
			rendered = normalStyle.Render("[ " + opt + " ]")
		}
		w := min(lipgloss.Width(rendered), b.ContentWidth()-x)
		hitWidth := w
		if i < len(d.options)-1 {
			hitWidth += gap // extend hit region to cover the gap for easier clicking
		}
		b.AddRegion(dialogIDOptPrefix+itoa(i), x, rowY, hitWidth, 1)
		sb.WriteString(rendered)
		if i < len(d.options)-1 {
			sb.WriteString("  ")
			x += w + gap
		} else {
			x += w
		}
	}
	b.Append("", sb.String())
}

// visibleOptionIndices returns the option indices in render order, accounting
// for fuzzy filtering on DialogSelect.
func (d *Dialog) visibleOptionIndices() []int {
	if d.filterEnabled && d.filteredIndices != nil {
		return d.filteredIndices
	}
	indices := make([]int, len(d.options))
	for i := range d.options {
		indices[i] = i
	}
	return indices
}

func (d *Dialog) helpText() string {
	switch d.dtype {
	case DialogInput:
		if d.hasSelect() {
			return "↑/↓: navigate • ←/→: change mode • space: toggle • enter: confirm • esc: cancel"
		}
		if d.checkboxLabel != "" || d.checkbox2Label != "" || d.checkbox3Label != "" {
			return "↑/↓: navigate • space: toggle • enter: confirm • esc: cancel"
		}
		return "enter: confirm • esc: cancel • click OK/Cancel"
	case DialogConfirm:
		return "←/→ or tab: choose • enter: confirm • esc: cancel"
	case DialogSelect:
		if d.filterEnabled {
			return "type to filter • ↑/↓ or tab: move • enter: select • esc: cancel"
		}
		if d.verticalLayout {
			return "↑/↓ or tab: move • enter: select • esc: cancel"
		}
		return "←/→ or tab: move • enter: select • esc: cancel"
	default:
		return "enter: confirm • esc: cancel"
	}
}

// itoa is a tiny stdlib-free int→string converter for option-ID suffixes.
// Avoids pulling fmt into the render hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// selectRegionID namespaces a select's hit regions per slot, so a click lands
// on the cycler the user actually aimed at.
func selectRegionID(base string, slot int) string {
	if slot == 0 {
		return base
	}
	return base + "-" + itoa(slot)
}
