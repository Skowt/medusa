package common

// Configuration builders for Dialog. Each returns the receiver so a dialog can
// be described in one chained expression at its construction site.

// SetValue sets the input text value. Call after Show() to pre-fill input
// (Show resets the value to empty). Only applies to DialogInput.
func (d *Dialog) SetValue(value string) *Dialog {
	if d.dtype == DialogInput {
		d.input.SetValue(value)
		d.input.CursorEnd()
	}
	return d
}

// SetMessage sets the dialog description/message text.
func (d *Dialog) SetMessage(msg string) *Dialog {
	d.message = msg
	return d
}

// SetOptionHints attaches a per-option hint, rendered as a dimmed line below the
// option row for whichever option is focused. Hints are matched to options by
// index; a short slice simply leaves the trailing options without a hint.
func (d *Dialog) SetOptionHints(hints []string) *Dialog {
	d.optionHints = hints
	return d
}

// optionHint returns the hint for option index i, or "" if there isn't one.
func (d *Dialog) optionHint(i int) string {
	if i < 0 || i >= len(d.optionHints) {
		return ""
	}
	return d.optionHints[i]
}

// SetInputTransform sets a transform function that will be applied to input text
func (d *Dialog) SetInputTransform(fn InputTransformFunc) *Dialog {
	d.inputTransform = fn
	return d
}

// SetInputValidate sets a validation function that runs on each keystroke
func (d *Dialog) SetInputValidate(fn InputValidateFunc) *Dialog {
	d.inputValidate = fn
	return d
}

// SetCheckbox adds a checkbox to the dialog (only for DialogInput).
// The label is shown next to the checkbox, and defaultValue sets the initial state.
func (d *Dialog) SetCheckbox(label string, defaultValue bool) *Dialog {
	d.checkboxLabel = label
	d.checkboxValue = defaultValue
	return d
}

// CheckboxValue returns the current checkbox state.
func (d *Dialog) CheckboxValue() bool {
	return d.checkboxValue
}

// SetCheckbox2 adds a second checkbox to the dialog (only for DialogInput).
func (d *Dialog) SetCheckbox2(label string, defaultValue bool) *Dialog {
	d.checkbox2Label = label
	d.checkbox2Value = defaultValue
	return d
}

// Checkbox2Value returns the current second checkbox state.
func (d *Dialog) Checkbox2Value() bool {
	return d.checkbox2Value
}

// SetCheckbox3 adds a third checkbox to the dialog (only for DialogInput).
func (d *Dialog) SetCheckbox3(label string, defaultValue bool) *Dialog {
	d.checkbox3Label = label
	d.checkbox3Value = defaultValue
	return d
}

// Checkbox3Value returns the current third checkbox state.
func (d *Dialog) Checkbox3Value() bool {
	return d.checkbox3Value
}

// SetInputHidden hides the text input field, useful for checkbox-only dialogs.
func (d *Dialog) SetInputHidden(hidden bool) *Dialog {
	d.inputHidden = hidden
	return d
}

// SetDefaultConfirm sets the default cursor position for confirm dialogs.
// If yes is true, the cursor defaults to "Yes"; otherwise it defaults to "No".
func (d *Dialog) SetDefaultConfirm(yes bool) *Dialog {
	if yes {
		d.cursor = 0
		d.defaultCursor = 0
	} else {
		d.cursor = 1
		d.defaultCursor = 1
	}
	return d
}

// SetVerticalLayout sets the dialog to render options vertically instead of horizontally.
func (d *Dialog) SetVerticalLayout(vertical bool) *Dialog {
	d.verticalLayout = vertical
	return d
}
