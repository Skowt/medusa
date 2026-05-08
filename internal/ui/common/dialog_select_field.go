package common

import "strings"

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

// SetSelect adds an inline cycler ("Label: ◀ Current ▶") to a DialogInput.
// defaultValue selects the matching option's index, falling back to 0.
func (d *Dialog) SetSelect(label string, options []SelectOption, defaultValue string) *Dialog {
	d.selectLabel = label
	d.selectOptions = options
	d.selectIndex = 0
	for i, opt := range options {
		if opt.Value == defaultValue {
			d.selectIndex = i
			break
		}
	}
	return d
}

// SelectValue returns the currently-selected option's Value, or "" if none.
func (d *Dialog) SelectValue() string {
	if d.selectIndex < 0 || d.selectIndex >= len(d.selectOptions) {
		return ""
	}
	return d.selectOptions[d.selectIndex].Value
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

// cycleSelect moves the select field's cursor by step, wrapping.
func (d *Dialog) cycleSelect(step int) {
	n := len(d.selectOptions)
	if n == 0 {
		return
	}
	d.selectIndex = (d.selectIndex + step + n) % n
}

// Focus management for DialogInput. Slots are numbered:
//
//	0 = input, 1 = checkbox1, 2 = checkbox2, 3 = checkbox3, 4 = select.
//
// advanceFocus walks the slot ring, skipping slots whose field is absent.
const focusSlotCount = 5

func (d *Dialog) hasFocusableFields() bool {
	return d.checkboxLabel != "" || d.checkbox2Label != "" || d.checkbox3Label != "" || d.selectLabel != ""
}

func (d *Dialog) currentFocusIdx() int {
	switch {
	case d.checkboxFocused:
		return 1
	case d.checkbox2Focused:
		return 2
	case d.checkbox3Focused:
		return 3
	case d.selectFocused:
		return 4
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
	case 4:
		return d.selectLabel != ""
	}
	return false
}

func (d *Dialog) setFocus(idx int) {
	d.checkboxFocused = idx == 1
	d.checkbox2Focused = idx == 2
	d.checkbox3Focused = idx == 3
	d.selectFocused = idx == 4
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
