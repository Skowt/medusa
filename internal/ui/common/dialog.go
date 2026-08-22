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
	CheckboxValue  bool   // Value of checkbox if dialog had one
	Checkbox2Value bool   // Value of second checkbox if dialog had one
	Checkbox3Value bool   // Value of third checkbox if dialog had one
	SelectValue    string // Value of select slot 0 if the dialog had one (DialogInput)
	Select2Value   string // Value of select slot 1 if the dialog had one (DialogInput)
	Select3Value   string // Value of select slot 2 if the dialog had one (DialogInput)
}

// DialogSelectChanged is emitted when a select field marked with
// SetSelectNotifiesChange cycles to a different option, so the dialog's owner
// can rebuild it around the new value — the New Tab dialog swapping in the
// chosen assistant's own fields. It fires on the change, not on submit, and
// only for slots that asked for it.
type DialogSelectChanged struct {
	ID    string
	Slot  int
	Value string
}

// InputTransformFunc transforms input text before it's added to the input field
type InputTransformFunc func(string) string

// InputValidateFunc validates input and returns an error message (empty = valid)
type InputValidateFunc func(string) string

// Dialog is a modal dialog component
type Dialog struct {
	// Configuration
	id          string
	dtype       DialogType
	title       string
	message     string
	options     []string
	optionHints []string // optional, parallel to options; hint for the focused option

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
	verticalLayout  bool // render options vertically instead of horizontally
	width           int
	height          int
	showKeymapHints bool

	// Checkboxes (for DialogInput). All three slots share the same shape;
	// hit regions are owned by the LineBuilder produced via build().
	checkboxLabel    string
	checkboxValue    bool
	checkboxFocused  bool
	checkbox2Label   string
	checkbox2Value   bool
	checkbox2Focused bool
	checkbox3Label   string
	checkbox3Value   bool
	checkbox3Focused bool

	// Optional muted description rendered under each checkbox (DialogInput only)
	checkboxDesc  string
	checkbox2Desc string
	checkbox3Desc string

	// When true, checkbox 2 is disabled (rendered muted, unclickable, not
	// togglable by space/enter) whenever checkbox 1 is unchecked. Used for
	// nested settings like "Allow unsandboxed commands" under "Sandboxed".
	checkbox2RequiresFirst bool

	// Select field (for DialogInput): a single-line cycler with description.
	// Inline select fields, rendered in slot order. Slot 0 is the dialog's
	// primary control; see selectField.
	sel [selectSlotCount]selectField

	// Input visibility
	inputHidden bool // Hide the text input field (checkbox-only dialog)

	// Default cursor position (restored by Show)
	defaultCursor int
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
	for i := range d.sel {
		d.sel[i].focused = false
	}
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
// ID returns the dialog's identifier, so an owner handling a result can tell
// whether the modal still on screen is its own.
func (d *Dialog) ID() string { return d.id }

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
	selectVal := d.SelectValue()
	select2Val := d.Select2Value()
	select3Val := d.Select3Value()
	logging.Info("Dialog submit input: id=%s value=%s confirmed=%v checkbox=%v checkbox2=%v checkbox3=%v select=%s select2=%s select3=%s", id, value, confirmed, checkboxVal, checkbox2Val, checkbox3Val, selectVal, select2Val, select3Val)
	return func() tea.Msg {
		return DialogResult{
			ID:             id,
			Confirmed:      confirmed,
			Value:          value,
			CheckboxValue:  checkboxVal,
			Checkbox2Value: checkbox2Val,
			Checkbox3Value: checkbox3Val,
			SelectValue:    selectVal,
			Select2Value:   select2Val,
			Select3Value:   select3Val,
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

	b := d.build()
	dialogW, dialogH := b.Size()
	dialogX := max(0, (d.width-dialogW)/2)
	dialogY := max(0, (d.height-dialogH)/2)
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}
	contentX, contentY := b.ContentOffset()
	localX := msg.X - dialogX - contentX
	localY := msg.Y - dialogY - contentY
	if localX < 0 || localY < 0 {
		return nil
	}

	hit := func(id string) bool {
		r, ok := b.RegionByID(id)
		return ok && r.Contains(localX, localY)
	}

	// Inline controls take priority over the row regions they sit on.
	if d.dtype == DialogInput {
		// Selects first: each slot owns three regions (row, left chevron,
		// right chevron), so they are checked per slot rather than in the
		// single switch below.
		for slot := range d.sel {
			if !d.sel[slot].present() {
				continue
			}
			switch {
			case hit(selectRegionID(dialogIDSelectLeft, slot)):
				d.setFocus(4 + slot)
				return d.cycleSelect(slot, -1)
			case hit(selectRegionID(dialogIDSelectRight, slot)),
				hit(selectRegionID(dialogIDSelectField, slot)):
				d.setFocus(4 + slot)
				return d.cycleSelect(slot, +1)
			}
		}
		switch {
		case d.checkboxLabel != "" && hit(dialogIDCheckbox1):
			d.checkboxValue = !d.checkboxValue
			return nil
		case d.checkbox2Label != "" && hit(dialogIDCheckbox2):
			if !d.checkbox2Disabled() {
				d.checkbox2Value = !d.checkbox2Value
			}
			return nil
		case d.checkbox3Label != "" && hit(dialogIDCheckbox3):
			d.checkbox3Value = !d.checkbox3Value
			return nil
		case hit(dialogIDOK):
			return d.submitInput(true)
		case hit(dialogIDCancel):
			return d.submitInput(false)
		}
		return nil
	}

	// Confirm/Select dispatch via option-N regions.
	for i, opt := range d.options {
		if hit(dialogIDOptPrefix + itoa(i)) {
			d.cursor = i
			if d.filterEnabled && d.filteredIndices != nil {
				for c, original := range d.filteredIndices {
					if original == i {
						d.cursor = c
						break
					}
				}
			}
			switch d.dtype {
			case DialogConfirm:
				return d.submitConfirm(i == 0)
			case DialogSelect:
				return d.submitSelect(i, opt)
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
