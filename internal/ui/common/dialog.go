package common

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
)

// validationDebounceMsg is sent after a short delay to trigger input validation.
// The seq field ensures stale debounce ticks are ignored when the user is still typing.
type validationDebounceMsg struct {
	seq int
}

const validationDebounceDelay = 100 * time.Millisecond

// DialogType identifies the type of dialog
type DialogType int

const (
	DialogNone DialogType = iota
	DialogInput
	DialogConfirm
	DialogSelect
)

// DialogResult is sent when a dialog is completed
type DialogResult struct {
	ID             string
	Confirmed      bool
	Value          string
	Values         []string // Multi-select results (e.g. file picker multi-select)
	Index          int
	CheckboxValue  bool // Value of checkbox if dialog had one
	Checkbox2Value bool // Value of second checkbox if dialog had one
	Checkbox3Value bool // Value of third checkbox if dialog had one
}

// InputTransformFunc transforms input text before it's added to the input field
type InputTransformFunc func(string) string

// InputValidateFunc validates input and returns an error message (empty = valid)
type InputValidateFunc func(string) string

// Dialog is a modal dialog component
type Dialog struct {
	// Configuration
	id      string
	dtype   DialogType
	title   string
	message string
	options []string

	// State
	visible   bool
	input     textinput.Model
	cursor    int
	confirmed bool

	// Input transformation and validation
	inputTransform InputTransformFunc
	inputValidate  InputValidateFunc
	validationErr  string
	validationSeq  int // incremented on input change; debounce tick only fires if seq matches

	// Fuzzy filter state
	filterEnabled   bool
	filterInput     textinput.Model
	filteredIndices []int // indices into options

	// Layout
	verticalLayout bool // render options vertically instead of horizontally
	width          int
	height         int
	optionHits     []dialogOptionHit
	// Display settings
	showKeymapHints bool

	// Checkbox (for DialogInput)
	checkboxLabel   string    // Label shown next to checkbox (empty = no checkbox)
	checkboxValue   bool      // Current checkbox state
	checkboxHit     HitRegion // Click region for checkbox
	checkboxFocused bool      // True when checkbox is focused (vs input)

	// Second checkbox (for DialogInput)
	checkbox2Label   string    // Label shown next to second checkbox (empty = no checkbox)
	checkbox2Value   bool      // Current second checkbox state
	checkbox2Hit     HitRegion // Click region for second checkbox
	checkbox2Focused bool      // True when second checkbox is focused

	// Third checkbox (for DialogInput)
	checkbox3Label   string    // Label shown next to third checkbox (empty = no checkbox)
	checkbox3Value   bool      // Current third checkbox state
	checkbox3Hit     HitRegion // Click region for third checkbox
	checkbox3Focused bool      // True when third checkbox is focused

	// Input visibility
	inputHidden bool // Hide the text input field (checkbox-only dialog)

	// Default cursor position (restored by Show)
	defaultCursor int
}

type dialogOptionHit struct {
	cursorIndex int
	optionIndex int
	region      HitRegion
}

// NewInputDialog creates a new input dialog
func NewInputDialog(id, title, placeholder string) *Dialog {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(40)
	ti.SetVirtualCursor(false)

	return &Dialog{
		id:    id,
		dtype: DialogInput,
		title: title,
		input: ti,
	}
}

// NewConfirmDialog creates a new confirmation dialog
func NewConfirmDialog(id, title, message string) *Dialog {
	return &Dialog{
		id:            id,
		dtype:         DialogConfirm,
		title:         title,
		message:       message,
		options:       []string{"Yes", "No"},
		cursor:        0, // Default to "Yes"
		defaultCursor: 0,
	}
}

// NewSelectDialog creates a new selection dialog
func NewSelectDialog(id, title, message string, options []string) *Dialog {
	return &Dialog{
		id:      id,
		dtype:   DialogSelect,
		title:   title,
		message: message,
		options: options,
		cursor:  0,
	}
}

// fuzzyMatch returns true if pattern fuzzy-matches target (case-insensitive)
func fuzzyMatch(pattern, target string) bool {
	if pattern == "" {
		return true
	}
	pattern = strings.ToLower(pattern)
	target = strings.ToLower(target)
	pi := 0
	for ti := 0; ti < len(target) && pi < len(pattern); ti++ {
		if target[ti] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

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

// transformInputMsg applies the input transform to key press and paste messages
func (d *Dialog) transformInputMsg(msg tea.Msg) tea.Msg {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if m.Text != "" {
			transformed := d.inputTransform(m.Text)
			if transformed != m.Text {
				m.Text = transformed
				return m
			}
		}
	case tea.PasteMsg:
		transformed := d.inputTransform(m.Content)
		if transformed != m.Content {
			m.Content = transformed
			return m
		}
	}
	return msg
}

// Show makes the dialog visible
func (d *Dialog) Show() {
	d.visible = true
	d.confirmed = false
	d.validationErr = ""
	d.cursor = d.defaultCursor
	d.checkboxFocused = false
	d.checkbox2Focused = false
	d.checkbox3Focused = false
	if d.dtype == DialogInput {
		d.input.SetValue("")
		d.input.Focus()
	}
	if d.filterEnabled {
		d.filterInput.SetValue("")
		d.filterInput.Focus()
		d.applyFilter()
	}
}

// applyFilter updates filteredIndices based on current filter input
func (d *Dialog) applyFilter() {
	query := d.filterInput.Value()
	d.filteredIndices = nil
	for i, opt := range d.options {
		if fuzzyMatch(query, opt) {
			d.filteredIndices = append(d.filteredIndices, i)
		}
	}
	// Clamp cursor to filtered range
	if d.cursor >= len(d.filteredIndices) {
		d.cursor = max(0, len(d.filteredIndices)-1)
	}
}

// Hide hides the dialog
func (d *Dialog) Hide() {
	d.visible = false
}

// Visible returns whether the dialog is visible
func (d *Dialog) Visible() bool {
	return d.visible
}

// dismiss builds a cancelled DialogResult.
func (d *Dialog) dismiss() tea.Cmd {
	d.visible = false
	id := d.id
	return func() tea.Msg {
		return DialogResult{ID: id, Confirmed: false}
	}
}

// submitInput builds a DialogResult for an input dialog.
// Returns nil (no-op) when confirmed is true but validation is failing.
func (d *Dialog) submitInput(confirmed bool) tea.Cmd {
	if confirmed && d.validationErr != "" {
		return nil
	}
	d.visible = false
	id := d.id
	value := d.input.Value()
	checkboxVal := d.checkboxValue
	checkbox2Val := d.checkbox2Value
	checkbox3Val := d.checkbox3Value
	logging.Info("Dialog submit input: id=%s value=%s confirmed=%v checkbox=%v checkbox2=%v checkbox3=%v", id, value, confirmed, checkboxVal, checkbox2Val, checkbox3Val)
	return func() tea.Msg {
		return DialogResult{
			ID:             id,
			Confirmed:      confirmed,
			Value:          value,
			CheckboxValue:  checkboxVal,
			Checkbox2Value: checkbox2Val,
			Checkbox3Value: checkbox3Val,
		}
	}
}

// submitConfirm builds a DialogResult for a confirm dialog.
func (d *Dialog) submitConfirm(confirmed bool) tea.Cmd {
	d.visible = false
	id := d.id
	return func() tea.Msg {
		return DialogResult{ID: id, Confirmed: confirmed}
	}
}

// submitSelect builds a DialogResult for a select dialog.
func (d *Dialog) submitSelect(index int, value string) tea.Cmd {
	d.visible = false
	id := d.id
	return func() tea.Msg {
		return DialogResult{
			ID:        id,
			Confirmed: true,
			Index:     index,
			Value:     value,
		}
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (d *Dialog) SetShowKeymapHints(show bool) {
	d.showKeymapHints = show
}

func (d *Dialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if !d.visible {
		return nil
	}

	lines := d.renderLines()
	if len(lines) == 0 {
		return nil
	}

	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	// Check for checkbox clicks
	if d.dtype == DialogInput && d.checkboxLabel != "" && d.checkboxHit.Contains(localX, localY) {
		d.checkboxValue = !d.checkboxValue
		return nil
	}
	if d.dtype == DialogInput && d.checkbox2Label != "" && d.checkbox2Hit.Contains(localX, localY) {
		d.checkbox2Value = !d.checkbox2Value
		return nil
	}
	if d.dtype == DialogInput && d.checkbox3Label != "" && d.checkbox3Hit.Contains(localX, localY) {
		d.checkbox3Value = !d.checkbox3Value
		return nil
	}

	for _, hit := range d.optionHits {
		if hit.region.Contains(localX, localY) {
			d.cursor = hit.cursorIndex

			switch d.dtype {
			case DialogInput:
				return d.submitInput(hit.optionIndex == 0)
			case DialogConfirm:
				return d.submitConfirm(hit.optionIndex == 0)
			case DialogSelect:
				return d.submitSelect(hit.optionIndex, d.options[hit.optionIndex])
			}
		}
	}

	return nil
}

// SetSize sets the dialog size
func (d *Dialog) SetSize(width, height int) {
	d.width = width
	d.height = height
	if d.dtype == DialogInput {
		d.input.SetWidth(min(40, width-10))
	}
	if d.dtype == DialogSelect && d.filterEnabled {
		d.filterInput.SetWidth(min(30, width-10))
	}
}
