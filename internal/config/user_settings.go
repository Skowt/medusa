package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// UISettings stores user-facing display preferences.
type UISettings struct {
	ShowKeymapHints              bool
	HideSidebar                  bool
	HideTerminal                 bool
	AutoStartAgent               bool
	SyncProfilePlugins           bool
	GlobalPermissions            bool
	AutoAddPermissions           bool
	LastProfile                  string // Most recently selected profile name
	LastWorkspace                string // ID of the workspace active when medusa last exited
	LastIsolated                 bool   // Last state of "run isolated" checkbox for new workspaces
	LastAllowUnsandboxedCommands bool   // Last state of "allow unsandboxed commands" checkbox
	LastPermissionMode           string // Last selected starting mode (default "auto")
	LastFullscreen               bool   // Last state of "Fullscreen TUI" checkbox (default on)
	LastAssistant                string // Assistant the New Tab dialog opens on (claude, codex)
	LastCodexSandbox             string // Last codex --sandbox policy
	LastCodexApproval            string // Last codex --ask-for-approval policy
	LastCodexSearch              bool   // Last state of the Codex "web search" checkbox
	Theme                        string // Theme ID, defaults to "gruvbox"
	TmuxServer                   string
	TmuxConfigPath               string
	TmuxSyncInterval             string
	TmuxPersistence              bool
	NotificationSound            string          // Sound name from /System/Library/Sounds (empty = none)
	IDE                          string          // Remembered IDE install launch path (.app bundle on macOS, binary on Linux)
	CompoundApprove              bool            // Auto-approve compound Bash commands via hook
	CollapsedGroups              map[string]bool // Dashboard group collapse state, keyed by group label ("" = Ungrouped)
}

func defaultUISettings() UISettings {
	return UISettings{
		ShowKeymapHints:    false,
		HideTerminal:       true,
		AutoStartAgent:     true,
		SyncProfilePlugins: true,
		GlobalPermissions:  true,
		AutoAddPermissions: false,
		LastPermissionMode: "auto",
		LastFullscreen:     true,
		LastAssistant:      "claude",
		LastCodexSandbox:   "workspace-write",
		LastCodexApproval:  "on-request",
		Theme:              "gruvbox",
		TmuxServer:         "",
		TmuxConfigPath:     "",
		TmuxSyncInterval:   "",
		TmuxPersistence:    true,
		NotificationSound:  "",
		CompoundApprove:    true,
		CollapsedGroups:    nil,
	}
}

func loadUISettings(path string) UISettings {
	settings := defaultUISettings()
	data, err := os.ReadFile(path)
	if err != nil {
		return settings
	}

	var raw struct {
		UI struct {
			ShowKeymapHints              *bool           `json:"show_keymap_hints"`
			HideSidebar                  *bool           `json:"hide_sidebar"`
			HideTerminal                 *bool           `json:"hide_terminal"`
			AutoStartAgent               *bool           `json:"auto_start_agent"`
			SyncProfilePlugins           *bool           `json:"sync_profile_plugins"`
			GlobalPermissions            *bool           `json:"global_permissions"`
			AutoAddPermissions           *bool           `json:"auto_add_permissions"`
			LastProfile                  *string         `json:"last_profile"`
			LastWorkspace                *string         `json:"last_workspace"`
			LastIsolated                 *bool           `json:"last_isolated"`
			LastSkipPermissions          *bool           `json:"last_skip_permissions"` // legacy → coalesced into LastPermissionMode
			LastAllowUnsandboxedCommands *bool           `json:"last_allow_unsandboxed_commands"`
			LastPermissionMode           *string         `json:"last_permission_mode"`
			LastFullscreen               *bool           `json:"last_fullscreen"`
			LastAssistant                *string         `json:"last_assistant"`
			LastCodexSandbox             *string         `json:"last_codex_sandbox"`
			LastCodexApproval            *string         `json:"last_codex_approval"`
			LastCodexSearch              *bool           `json:"last_codex_search"`
			Theme                        *string         `json:"theme"`
			TmuxServer                   *string         `json:"tmux_server"`
			TmuxConfigPath               *string         `json:"tmux_config"`
			TmuxSyncInterval             *string         `json:"tmux_sync_interval"`
			TmuxPersistence              *bool           `json:"tmux_persistence"`
			NotificationSound            *string         `json:"notification_sound"`
			IDE                          *string         `json:"ide"`
			CompoundApprove              *bool           `json:"compound_approve"`
			CollapsedGroups              map[string]bool `json:"collapsed_groups"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return settings
	}
	if raw.UI.ShowKeymapHints != nil {
		settings.ShowKeymapHints = *raw.UI.ShowKeymapHints
	}
	if raw.UI.HideSidebar != nil {
		settings.HideSidebar = *raw.UI.HideSidebar
	}
	if raw.UI.HideTerminal != nil {
		settings.HideTerminal = *raw.UI.HideTerminal
	}
	if raw.UI.AutoStartAgent != nil {
		settings.AutoStartAgent = *raw.UI.AutoStartAgent
	}
	if raw.UI.SyncProfilePlugins != nil {
		settings.SyncProfilePlugins = *raw.UI.SyncProfilePlugins
	}
	if raw.UI.GlobalPermissions != nil {
		settings.GlobalPermissions = *raw.UI.GlobalPermissions
	}
	if raw.UI.AutoAddPermissions != nil {
		settings.AutoAddPermissions = *raw.UI.AutoAddPermissions
	}
	if raw.UI.LastProfile != nil {
		settings.LastProfile = *raw.UI.LastProfile
	}
	if raw.UI.LastWorkspace != nil {
		settings.LastWorkspace = *raw.UI.LastWorkspace
	}
	if raw.UI.LastIsolated != nil {
		settings.LastIsolated = *raw.UI.LastIsolated
	}
	if raw.UI.LastAllowUnsandboxedCommands != nil {
		settings.LastAllowUnsandboxedCommands = *raw.UI.LastAllowUnsandboxedCommands
	}
	if raw.UI.LastPermissionMode != nil && *raw.UI.LastPermissionMode != "" {
		settings.LastPermissionMode = *raw.UI.LastPermissionMode
	} else if raw.UI.LastSkipPermissions != nil && *raw.UI.LastSkipPermissions {
		// Legacy: skip_permissions=true mapped to bypassPermissions.
		settings.LastPermissionMode = "bypassPermissions"
	}
	if raw.UI.LastFullscreen != nil {
		settings.LastFullscreen = *raw.UI.LastFullscreen
	}
	if raw.UI.LastAssistant != nil && *raw.UI.LastAssistant != "" {
		settings.LastAssistant = *raw.UI.LastAssistant
	}
	if raw.UI.LastCodexSandbox != nil && *raw.UI.LastCodexSandbox != "" {
		settings.LastCodexSandbox = *raw.UI.LastCodexSandbox
	}
	if raw.UI.LastCodexApproval != nil && *raw.UI.LastCodexApproval != "" {
		settings.LastCodexApproval = *raw.UI.LastCodexApproval
	}
	if raw.UI.LastCodexSearch != nil {
		settings.LastCodexSearch = *raw.UI.LastCodexSearch
	}
	if raw.UI.Theme != nil {
		settings.Theme = *raw.UI.Theme
	}
	if raw.UI.TmuxServer != nil {
		settings.TmuxServer = *raw.UI.TmuxServer
	}
	if raw.UI.TmuxConfigPath != nil {
		settings.TmuxConfigPath = *raw.UI.TmuxConfigPath
	}
	if raw.UI.TmuxSyncInterval != nil {
		settings.TmuxSyncInterval = *raw.UI.TmuxSyncInterval
	}
	if raw.UI.TmuxPersistence != nil {
		settings.TmuxPersistence = *raw.UI.TmuxPersistence
	}
	if raw.UI.NotificationSound != nil {
		settings.NotificationSound = *raw.UI.NotificationSound
	}
	if raw.UI.IDE != nil {
		settings.IDE = *raw.UI.IDE
	}
	if raw.UI.CompoundApprove != nil {
		settings.CompoundApprove = *raw.UI.CompoundApprove
	}
	if raw.UI.CollapsedGroups != nil {
		settings.CollapsedGroups = raw.UI.CollapsedGroups
	}
	return settings
}

func saveUISettings(path string, settings UISettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload := map[string]any{}
	if existing, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(existing, &payload)
	}

	ui, ok := payload["ui"].(map[string]any)
	if !ok || ui == nil {
		ui = map[string]any{}
	}
	ui["show_keymap_hints"] = settings.ShowKeymapHints
	ui["hide_sidebar"] = settings.HideSidebar
	ui["hide_terminal"] = settings.HideTerminal
	ui["auto_start_agent"] = settings.AutoStartAgent
	ui["sync_profile_plugins"] = settings.SyncProfilePlugins
	ui["global_permissions"] = settings.GlobalPermissions
	ui["auto_add_permissions"] = settings.AutoAddPermissions
	ui["last_profile"] = settings.LastProfile
	ui["last_workspace"] = settings.LastWorkspace
	delete(ui, "last_allow_edits")      // legacy field, removed in favor of per-tab settings
	delete(ui, "last_skip_permissions") // legacy field, replaced by last_permission_mode
	ui["last_isolated"] = settings.LastIsolated
	ui["last_allow_unsandboxed_commands"] = settings.LastAllowUnsandboxedCommands
	ui["last_permission_mode"] = settings.LastPermissionMode
	ui["last_fullscreen"] = settings.LastFullscreen
	ui["last_assistant"] = settings.LastAssistant
	ui["last_codex_sandbox"] = settings.LastCodexSandbox
	ui["last_codex_approval"] = settings.LastCodexApproval
	ui["last_codex_search"] = settings.LastCodexSearch
	ui["theme"] = settings.Theme
	ui["tmux_server"] = settings.TmuxServer
	ui["tmux_config"] = settings.TmuxConfigPath
	ui["tmux_sync_interval"] = settings.TmuxSyncInterval
	ui["tmux_persistence"] = settings.TmuxPersistence
	ui["notification_sound"] = settings.NotificationSound
	ui["ide"] = settings.IDE
	ui["compound_approve"] = settings.CompoundApprove
	ui["collapsed_groups"] = settings.CollapsedGroups
	payload["ui"] = ui

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SaveUISettings persists UI settings to the config file.
func (c *Config) SaveUISettings() error {
	if c == nil || c.Paths == nil {
		return nil
	}
	return saveUISettings(c.Paths.ConfigPath, c.UI)
}
