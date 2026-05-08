package common

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update handles messages
func (d *Dialog) Update(msg tea.Msg) (*Dialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case validationDebounceMsg:
		if d.dtype == DialogInput && d.inputValidate != nil && msg.seq == d.validationSeq {
			d.validationErr = d.inputValidate(d.input.Value())
		}
		return d, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if cmd := d.handleClick(msg); cmd != nil {
				return d, cmd
			}
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			return d, d.dismiss()

		case msg.Text == " ":
			// Toggle checkbox when focused
			if d.dtype == DialogInput && d.checkboxLabel != "" && d.checkboxFocused {
				d.checkboxValue = !d.checkboxValue
				return d, nil
			}
			if d.dtype == DialogInput && d.checkbox2Label != "" && d.checkbox2Focused && !d.checkbox2Disabled() {
				d.checkbox2Value = !d.checkbox2Value
				return d, nil
			}
			if d.dtype == DialogInput && d.checkbox3Label != "" && d.checkbox3Focused {
				d.checkbox3Value = !d.checkbox3Value
				return d, nil
			}
			// Otherwise let space pass through to text input

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			// Toggle checkbox when focused instead of submitting
			if d.dtype == DialogInput && d.checkboxLabel != "" && d.checkboxFocused {
				d.checkboxValue = !d.checkboxValue
				return d, nil
			}
			if d.dtype == DialogInput && d.checkbox2Label != "" && d.checkbox2Focused && !d.checkbox2Disabled() {
				d.checkbox2Value = !d.checkbox2Value
				return d, nil
			}
			if d.dtype == DialogInput && d.checkbox3Label != "" && d.checkbox3Focused {
				d.checkbox3Value = !d.checkbox3Value
				return d, nil
			}
			switch d.dtype {
			case DialogInput:
				return d, d.submitInput(true)
			case DialogConfirm:
				return d, d.submitConfirm(d.cursor == 0)
			case DialogSelect:
				// For filtered dialogs, resolve the original index
				var originalIdx int
				var value string
				if d.filterEnabled && len(d.filteredIndices) > 0 {
					originalIdx = d.filteredIndices[d.cursor]
					value = d.options[originalIdx]
				} else if !d.filterEnabled && d.cursor < len(d.options) {
					originalIdx = d.cursor
					value = d.options[d.cursor]
				} else {
					return d, d.dismiss()
				}
				return d, d.submitSelect(originalIdx, value)
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down"))):
			// Handle navigation in DialogInput with checkboxes / select field.
			if d.dtype == DialogInput && d.hasFocusableFields() {
				d.advanceFocus(+1)
				return d, nil
			}
			if d.dtype != DialogInput {
				maxLen := len(d.options)
				if d.filterEnabled {
					maxLen = len(d.filteredIndices)
				}
				if maxLen > 0 {
					d.cursor = (d.cursor + 1) % maxLen
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up"))):
			// Reverse navigation in DialogInput with checkboxes / select field.
			if d.dtype == DialogInput && d.hasFocusableFields() {
				d.advanceFocus(-1)
				return d, nil
			}
			if d.dtype != DialogInput {
				maxLen := len(d.options)
				if d.filterEnabled {
					maxLen = len(d.filteredIndices)
				}
				if maxLen > 0 {
					d.cursor--
					if d.cursor < 0 {
						d.cursor = maxLen - 1
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
			if d.dtype == DialogInput && d.selectFocused {
				d.cycleSelect(-1)
				return d, nil
			}
			if d.dtype == DialogConfirm || (d.dtype == DialogSelect && !d.filterEnabled && !d.verticalLayout) {
				maxLen := len(d.options)
				if maxLen > 0 {
					d.cursor--
					if d.cursor < 0 {
						d.cursor = maxLen - 1
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right"))):
			if d.dtype == DialogInput && d.selectFocused {
				d.cycleSelect(+1)
				return d, nil
			}
			if d.dtype == DialogConfirm || (d.dtype == DialogSelect && !d.filterEnabled && !d.verticalLayout) {
				maxLen := len(d.options)
				if maxLen > 0 {
					d.cursor = (d.cursor + 1) % maxLen
				}
			}
		}
	}

	// Update text input if applicable (skip when checkbox is focused)
	if d.dtype == DialogInput && !d.checkboxFocused && !d.checkbox2Focused && !d.checkbox3Focused {
		// Transform incoming text if transform function is set
		if d.inputTransform != nil {
			msg = d.transformInputMsg(msg)
		}

		oldVal := d.input.Value()
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)

		// Debounce validation: only schedule when the value actually changes,
		// and wait briefly so rapid keystrokes don't each spawn git processes.
		if d.inputValidate != nil && d.input.Value() != oldVal {
			d.validationSeq++
			seq := d.validationSeq
			debounceCmd := tea.Tick(validationDebounceDelay, func(time.Time) tea.Msg {
				return validationDebounceMsg{seq: seq}
			})
			return d, tea.Batch(cmd, debounceCmd)
		}

		return d, cmd
	}

	// Update filter input for filtered select dialogs
	if d.dtype == DialogSelect && d.filterEnabled {
		oldValue := d.filterInput.Value()
		var cmd tea.Cmd
		d.filterInput, cmd = d.filterInput.Update(msg)
		// Reapply filter if input changed
		if d.filterInput.Value() != oldValue {
			d.applyFilter()
		}
		return d, cmd
	}

	return d, nil
}
