package common

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// SelectOption is a choice in a Dialog's inline select field.
// Value is the machine value returned via DialogResult.SelectValue.
// Label is the human label shown in the cycler.
// Description is a short paragraph rendered below the cycler when this
// option is the current selection.
type SelectOption struct {
	Value       string
	Label       string
	Description string
}

// selectSlotCount is how many inline select fields a DialogInput can carry.
// The New Tab dialog uses up to three: assistant, starting mode, and sandbox.
const selectSlotCount = 3

// selectField is one inline cycler. Slots render in order, so slot 0 is the
// dialog's primary control.
type selectField struct {
	label   string
	options []SelectOption
	index   int
	focused bool
	// notify makes a change to this field emit DialogSelectChanged, for
	// callers that rebuild the dialog around the new value.
	notify bool
	// disabled keeps the value visible for context but removes the field from
	// keyboard/mouse interaction and the focus ring.
	disabled bool
}

func (f *selectField) present() bool {
	return f.label != "" && len(f.options) > 0
}

func (f *selectField) current() SelectOption {
	if f.index < 0 || f.index >= len(f.options) {
		return SelectOption{}
	}
	return f.options[f.index]
}

func (f *selectField) value() string {
	return f.current().Value
}

// SetSelect adds an inline cycler ("Label: ◀ Current ▶") to a DialogInput.
// defaultValue selects the matching option's index, falling back to 0.
func (d *Dialog) SetSelect(label string, options []SelectOption, defaultValue string) *Dialog {
	return d.setSelectSlot(0, label, options, defaultValue)
}

// SetSelect2 adds a second cycler, rendered below the first.
func (d *Dialog) SetSelect2(label string, options []SelectOption, defaultValue string) *Dialog {
	return d.setSelectSlot(1, label, options, defaultValue)
}

// SetSelect3 adds a third cycler, rendered below the second.
func (d *Dialog) SetSelect3(label string, options []SelectOption, defaultValue string) *Dialog {
	return d.setSelectSlot(2, label, options, defaultValue)
}

func (d *Dialog) setSelectSlot(slot int, label string, options []SelectOption, defaultValue string) *Dialog {
	if slot < 0 || slot >= selectSlotCount {
		return d
	}
	field := &d.sel[slot]
	field.label = label
	field.options = options
	field.index = 0
	for i, opt := range options {
		if opt.Value == defaultValue {
			field.index = i
			break
		}
	}
	return d
}

// SetSelectNotifiesChange makes a slot emit DialogSelectChanged whenever the
// user cycles it, so the owner can swap the rest of the dialog's fields to
// match — what the New Tab dialog does when the assistant changes. Without it
// a cycle is silent until submit.
func (d *Dialog) SetSelectNotifiesChange(slot int) *Dialog {
	if slot >= 0 && slot < selectSlotCount {
		d.sel[slot].notify = true
	}
	return d
}

// SetSelectDisabled renders a select slot muted and prevents it from being
// focused or cycled. Its current value is still returned on submit.
func (d *Dialog) SetSelectDisabled(slot int, disabled bool) *Dialog {
	if slot >= 0 && slot < selectSlotCount {
		d.sel[slot].disabled = disabled
	}
	return d
}

// SelectValue returns slot 0's selected Value, or "" if it has none.
func (d *Dialog) SelectValue() string {
	return d.sel[0].value()
}

// Select2Value returns slot 1's selected Value, or "" if it has none.
func (d *Dialog) Select2Value() string {
	return d.sel[1].value()
}

// Select3Value returns slot 2's selected Value, or "" if it has none.
func (d *Dialog) Select3Value() string {
	return d.sel[2].value()
}

// FocusSelect puts the focus ring on a select slot, so the arrow keys act on it
// straight away. Callers that rebuild a dialog around a select use it to land
// the user back on the cycler they just moved.
func (d *Dialog) FocusSelect(slot int) *Dialog {
	if slot >= 0 && slot < selectSlotCount && d.sel[slot].present() && !d.sel[slot].disabled {
		d.setFocus(4 + slot)
	}
	return d
}

// hasSelect reports whether any select slot is in use.
func (d *Dialog) hasSelect() bool {
	for i := range d.sel {
		if d.sel[i].present() {
			return true
		}
	}
	return false
}

// SetCheckbox2RequiresFirst makes checkbox 2 disabled (rendered muted and
// unable to be toggled) whenever checkbox 1 is unchecked. Useful for nested
// settings — e.g. "Allow unsandboxed commands" only matters when "Sandboxed"
// is on.
func (d *Dialog) SetCheckbox2RequiresFirst(requires bool) *Dialog {
	d.checkbox2RequiresFirst = requires
	return d
}

// checkbox2Disabled reports whether checkbox 2 is currently disabled by the
// SetCheckbox2RequiresFirst flag.
func (d *Dialog) checkbox2Disabled() bool {
	return d.checkbox2RequiresFirst && !d.checkboxValue
}

// SetCheckboxDescription attaches a muted multi-line description rendered
// below the corresponding checkbox. idx is 1-based (1, 2, or 3).
func (d *Dialog) SetCheckboxDescription(idx int, desc string) *Dialog {
	switch idx {
	case 1:
		d.checkboxDesc = desc
	case 2:
		d.checkbox2Desc = desc
	case 3:
		d.checkbox3Desc = desc
	}
	return d
}

// wordWrap breaks text on whitespace into lines of at most width columns.
// Words longer than width get their own line. Used for description text
// under checkboxes/select where lipgloss's Width+MarginLeft combo proved
// unreliable (it preserved indent on 2-line wraps but not 3+).
func wordWrap(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var lines []string
	// An explicit newline is a break the author asked for, so it survives the
	// wrap. Without this, strings.Fields folds it away and two parallel
	// statements ("On: ... " / "Off: ...") reflow into one paragraph the reader
	// has to parse rather than scan.
	for _, para := range strings.Split(text, "\n") {
		lines = append(lines, wrapParagraph(para, width)...)
	}
	return lines
}

// wrapParagraph greedily wraps one newline-free run of text.
func wrapParagraph(text string, width int) []string {
	var lines []string
	var cur strings.Builder
	for _, word := range strings.Fields(text) {
		if cur.Len() == 0 {
			cur.WriteString(word)
			continue
		}
		if cur.Len()+1+len(word) <= width {
			cur.WriteByte(' ')
			cur.WriteString(word)
			continue
		}
		lines = append(lines, cur.String())
		cur.Reset()
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// cycleSelect moves a select slot's cursor by step, wrapping, and returns the
// change notification when that slot asked for one.
func (d *Dialog) cycleSelect(slot, step int) tea.Cmd {
	if slot < 0 || slot >= selectSlotCount {
		return nil
	}
	field := &d.sel[slot]
	if field.disabled {
		return nil
	}
	n := len(field.options)
	if n == 0 {
		return nil
	}
	before := field.index
	field.index = (field.index + step + n) % n
	if !field.notify || field.index == before {
		return nil
	}
	id, value := d.id, field.value()
	return func() tea.Msg {
		return DialogSelectChanged{ID: id, Slot: slot, Value: value}
	}
}

// focusedSelectSlot returns the slot the focus ring currently sits on, or -1.
func (d *Dialog) focusedSelectSlot() int {
	for i := range d.sel {
		if d.sel[i].focused {
			return i
		}
	}
	return -1
}

// Focus management for DialogInput. Slots are numbered:
//
//	0 = input, 1 = checkbox1, 2 = checkbox2, 3 = checkbox3,
//	4 = select 0, 5 = select 1, 6 = select 2.
//
// advanceFocus walks the slot ring, skipping slots whose field is absent.
const focusSlotCount = 4 + selectSlotCount

// selectSlotForFocus maps a focus slot to its select slot, or -1.
func selectSlotForFocus(idx int) int {
	if idx < 4 || idx >= 4+selectSlotCount {
		return -1
	}
	return idx - 4
}

func (d *Dialog) hasFocusableFields() bool {
	return d.checkboxLabel != "" || d.checkbox2Label != "" || d.checkbox3Label != "" || d.hasSelect()
}

func (d *Dialog) currentFocusIdx() int {
	switch {
	case d.checkboxFocused:
		return 1
	case d.checkbox2Focused:
		return 2
	case d.checkbox3Focused:
		return 3
	}
	if slot := d.focusedSelectSlot(); slot >= 0 {
		return 4 + slot
	}
	return 0
}

func (d *Dialog) fieldExists(idx int) bool {
	switch idx {
	case 0:
		return !d.inputHidden
	case 1:
		return d.checkboxLabel != ""
	case 2:
		return d.checkbox2Label != ""
	case 3:
		return d.checkbox3Label != ""
	}
	if slot := selectSlotForFocus(idx); slot >= 0 {
		return d.sel[slot].present() && !d.sel[slot].disabled
	}
	return false
}

func (d *Dialog) setFocus(idx int) {
	d.checkboxFocused = idx == 1
	d.checkbox2Focused = idx == 2
	d.checkbox3Focused = idx == 3
	for i := range d.sel {
		d.sel[i].focused = selectSlotForFocus(idx) == i
	}
	if idx == 0 {
		d.input.Focus()
	} else {
		d.input.Blur()
	}
}

func (d *Dialog) advanceFocus(step int) {
	cur := d.currentFocusIdx()
	for i := 0; i < focusSlotCount; i++ {
		cur = (cur + step + focusSlotCount) % focusSlotCount
		if d.fieldExists(cur) {
			d.setFocus(cur)
			return
		}
	}
}
