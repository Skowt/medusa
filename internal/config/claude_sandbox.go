package config

import "fmt"

// ClaudeSandboxSettingsJSON returns the JSON passed to `claude --settings`
// when an agent is launched with the Sandboxed toggle on. It enables
// Claude Code's built-in sandbox and refuses to start if the sandbox
// can't initialize. allowUnsandboxedCommands is wired from the per-tab
// "Allow unsandboxed commands" toggle; when false, every command must
// be sandboxed. Only the `sandbox` key is set, so existing profile
// settings (permissions, hooks, etc.) are preserved by Claude Code's
// key-level merge.
func ClaudeSandboxSettingsJSON(allowUnsandboxedCommands bool) string {
	return fmt.Sprintf(
		`{"sandbox":{"enabled":true,"failIfUnavailable":true,"allowUnsandboxedCommands":%t}}`,
		allowUnsandboxedCommands,
	)
}
