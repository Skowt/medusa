package data

import "encoding/json"

// legacyPermissionFields captures pre-permission-mode JSON tags so
// `skip_permissions: true` payloads written before the dropdown landed
// keep their bypass intent after upgrade. Once unmarshalled, the field
// is coalesced into PermissionMode and never written back.
type legacyPermissionFields struct {
	SkipPermissions *bool `json:"skip_permissions,omitempty"`
}

func resolvePermissionMode(current string, legacy *bool) string {
	if current != "" {
		return current
	}
	if legacy != nil && *legacy {
		return "bypassPermissions"
	}
	return ""
}

// UnmarshalJSON reads TabInfo with legacy skip_permissions migration.
func (t *TabInfo) UnmarshalJSON(b []byte) error {
	type alias TabInfo
	aux := struct {
		*alias
		legacyPermissionFields
	}{alias: (*alias)(t)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	t.PermissionMode = resolvePermissionMode(t.PermissionMode, aux.SkipPermissions)
	return nil
}

// UnmarshalJSON reads Workspace with legacy skip_permissions migration.
func (w *Workspace) UnmarshalJSON(b []byte) error {
	type alias Workspace
	aux := struct {
		*alias
		legacyPermissionFields
	}{alias: (*alias)(w)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	w.PermissionMode = resolvePermissionMode(w.PermissionMode, aux.SkipPermissions)
	return nil
}
