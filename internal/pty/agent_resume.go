package pty

import (
	"os"
	"path/filepath"
	"strings"

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

// claudeProjectDir returns the directory Claude Code keeps cwd's transcripts
// in. Claude encodes the path by replacing every '/' and '.' with '-', so
// /Users/me/.medusa/workspaces/ws becomes -Users-me--medusa-workspaces-ws.
// The encoding belongs to Claude Code, not to us, so every caller must read a
// miss as "cannot tell" rather than "no conversations" and degrade to the
// behaviour it would have had without this check.
func claudeProjectDir(configDir, cwd string) string {
	if configDir == "" || cwd == "" {
		return ""
	}
	encoded := strings.NewReplacer("/", "-", ".", "-").Replace(filepath.Clean(cwd))
	return filepath.Join(configDir, "projects", encoded)
}

// claudeCwdHasConversation reports whether cwd's project directory holds at
// least one transcript, i.e. whether `claude --continue` has anything to
// resume. Without the check a --continue in a worktree that has never been
// conversed in exits with "No conversation found" and drops the tab to a bare
// shell.
func claudeCwdHasConversation(configDir, cwd string) bool {
	dir := claudeProjectDir(configDir, cwd)
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			return true
		}
	}
	return false
}

// claudeSessionArgs returns the session flags for the claude command line.
// The session ID is pre-generated at tab creation (--session-id); the
// conversation file only appears once the user converses. Resuming an ID
// without a conversation makes claude exit with "No conversation found" and
// drops the tab to a bare shell, so that case must resolve to something else.
//
// The fallback is deliberately --continue rather than a fresh session under
// the same ID. A recorded ID with no transcript does not mean the tab has no
// conversation: a stray SessionStart hook can overwrite the tab's ID with a
// session that never owned a transcript (see handleSessionStart), and starting
// fresh then discards a live conversation silently. --continue picks the most
// recent conversation in the worktree, which is that conversation in every
// case we have observed. Only when the worktree has no transcript at all does
// a fresh session under the recorded ID remain the right answer.
//
// The trade is a narrow one: restarting in the window between a /clear and the
// first message of the new session continues the cleared conversation instead
// of opening an empty one, because the post-clear session has no transcript
// yet. Handing back a conversation the user cleared is a far smaller loss than
// silently discarding one they did not.
func claudeSessionArgs(configDir string, opts AgentOptions) string {
	if opts.ClaudeSessionID == "" {
		return ""
	}
	if opts.Resume {
		if claudeConversationExists(configDir, opts.ClaudeSessionID) {
			return " --resume " + shellutil.Quote(opts.ClaudeSessionID)
		}
		if claudeCwdHasConversation(configDir, opts.Cwd) {
			logging.Warn("No conversation for Claude session %s; continuing the most recent conversation in %s instead", opts.ClaudeSessionID, opts.Cwd)
			return " --continue"
		}
		logging.Info("No conversation found for Claude session %s and none to continue in %s; starting fresh with the same session ID", opts.ClaudeSessionID, opts.Cwd)
	}
	return " --session-id " + shellutil.Quote(opts.ClaudeSessionID)
}
