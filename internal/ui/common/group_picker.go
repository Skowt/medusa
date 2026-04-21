package common

// NewGroupOption is the sentinel option at the end of the group picker that
// triggers a text-input fallback for creating a new group.
const NewGroupOption = "New group..."

// UngroupedOption is the sentinel at the top of the group picker that clears
// the workspace's group (moving it to the Ungrouped section).
const UngroupedOption = "Ungrouped"

// NewGroupPicker creates a select dialog listing existing groups, with
// "Ungrouped" at the top (to clear an assignment) and "New group..." at the
// bottom (to create a fresh label via a follow-up input dialog). If
// currentGroup matches one of the entries, the cursor starts on that entry;
// otherwise it starts on "Ungrouped".
func NewGroupPicker(id string, groups []string, currentGroup string) *Dialog {
	options := make([]string, 0, len(groups)+2)
	options = append(options, UngroupedOption)
	options = append(options, groups...)
	options = append(options, NewGroupOption)

	cursor := 0 // default to Ungrouped
	if currentGroup == "" {
		cursor = 0
	} else {
		for i, g := range options {
			if g == currentGroup {
				cursor = i
				break
			}
		}
	}

	return &Dialog{
		id:             id,
		dtype:          DialogSelect,
		title:          "Set Group",
		message:        "Pick an existing group, clear it, or create a new one.",
		options:        options,
		cursor:         cursor,
		verticalLayout: true,
	}
}
