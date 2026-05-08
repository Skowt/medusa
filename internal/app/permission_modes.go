package app

import "github.com/Skowt/medusa/internal/ui/common"

// permissionModeOptions returns the four modes exposed by the customize-tab
// dialog's "Starting Mode" cycler. Order = the order users cycle through.
// Descriptions are paraphrased from
// https://code.claude.com/docs/en/permissions.md.
func permissionModeOptions() []common.SelectOption {
	return []common.SelectOption{
		{
			Value:       "auto",
			Label:       "Auto",
			Description: "Auto-approves tool calls; a background classifier checks each action against your request.",
		},
		{
			Value:       "acceptEdits",
			Label:       "Accept Edits",
			Description: "Auto-accepts file edits and common filesystem commands within the workspace.",
		},
		{
			Value:       "plan",
			Label:       "Plan",
			Description: "Read-only exploration. Claude can read files and run read-only commands but won't edit anything.",
		},
		{
			Value:       "bypassPermissions",
			Label:       "Bypass Permissions",
			Description: "Skips all permission prompts. Use only inside a sandbox.",
		},
	}
}

// defaultPermissionMode returns the user's last-selected mode, falling back
// to "auto" when nothing's been saved yet.
func defaultPermissionMode(last string) string {
	if last == "" {
		return "auto"
	}
	return last
}
