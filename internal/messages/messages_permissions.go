package messages

// PermissionWatcherEvent is sent when a watched settings.local.json changes
type PermissionWatcherEvent struct {
	Root     string
	NewAllow []string
}

// PermissionDetected is sent when new permissions are found in a workspace
type PermissionDetected struct {
	WorkspaceRoot string
	WorkspaceName string
	NewAllow      []string
}

// ShowPermissionsDialog requests showing the pending permissions dialog
type ShowPermissionsDialog struct{}

// PermissionsDialogResult contains the user's actions on pending permissions
type PermissionsDialogResult struct {
	Actions []PermissionAction
}

// PermissionAction represents the user's choice for a single pending permission
type PermissionAction struct {
	Permission string
	Action     PermissionActionType
}

// PermissionActionType identifies how to handle a detected permission
type PermissionActionType int

const (
	PermissionAllow PermissionActionType = iota
	PermissionDeny
	PermissionSkip
)

// PermissionsEditorResult contains the updated allow/deny lists from the editor
type PermissionsEditorResult struct {
	Confirmed bool
	Allow     []string
	Deny      []string
}
