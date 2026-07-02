package pty

import (
	"os"
	"path/filepath"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/shellutil"
)

// claudeConversationExists reports whether a conversation file for sessionID
// exists anywhere under configDir/projects. Session IDs are UUIDs, so a scan
// across project directories cannot produce false positives, and it avoids
// depending on Claude Code's cwd→directory-name encoding.
func claudeConversationExists(configDir, sessionID string) bool {
	if configDir == "" || sessionID == "" {
		return false
	}
	projects, err := os.ReadDir(filepath.Join(configDir, "projects"))
	if err != nil {
		return false
	}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		conversation := filepath.Join(configDir, "projects", project.Name(), sessionID+".jsonl")
		if _, err := os.Stat(conversation); err == nil {
			return true
		}
	}
	return false
}

// claudeSessionArgs returns the session flags for the claude command line.
// The session ID is pre-generated at tab creation (--session-id); the
// conversation file only appears once the user converses. Resuming an ID
// without a conversation makes claude exit with "No conversation found" and
// drops the tab to a bare shell, so in that case start fresh under the same
// ID instead.
func claudeSessionArgs(configDir string, opts AgentOptions) string {
	if opts.ClaudeSessionID == "" {
		return ""
	}
	if opts.Resume {
		if claudeConversationExists(configDir, opts.ClaudeSessionID) {
			return " --resume " + shellutil.Quote(opts.ClaudeSessionID)
		}
		logging.Info("No conversation found for Claude session %s; starting fresh with the same session ID", opts.ClaudeSessionID)
	}
	return " --session-id " + shellutil.Quote(opts.ClaudeSessionID)
}
