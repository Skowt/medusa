package app

import (
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/ui/common"
)

// Assistants the New Tab dialog can launch. The values are what ride in
// data.TabInfo.Assistant and pty.AgentType, so they must not change.
const (
	assistantClaude = "claude"
	assistantCodex  = "codex"
)

// assistantOptions returns the "Assistant" cycler's choices. Cycling it
// rebuilds the dialog around the chosen assistant, since the fields below it
// belong to that assistant and to no other.
func assistantOptions() []common.SelectOption {
	return []common.SelectOption{
		{
			Value:       assistantClaude,
			Label:       "Claude Code",
			Description: "Anthropic's Claude Code, scoped to this workspace's profile.",
		},
		{
			Value:       assistantCodex,
			Label:       "Codex",
			Description: "OpenAI's Codex CLI. Its first tab in a profile asks you to trust Medusa's activity hooks once.",
		},
	}
}

// defaultAssistant returns the assistant the dialog opens on: the last one
// used, falling back to Claude.
func defaultAssistant(last string) string {
	if last == assistantCodex {
		return assistantCodex
	}
	return assistantClaude
}

// codexSandboxOptions returns the "Sandbox" cycler's choices, the policies
// codex --sandbox accepts. Descriptions are paraphrased from Codex's own
// sandbox documentation.
func codexSandboxOptions() []common.SelectOption {
	return []common.SelectOption{
		{
			Value:       appPty.CodexSandboxWorkspace,
			Label:       "Workspace Write",
			Description: "Codex may read anywhere and write inside the worktree. Network access still needs approval.",
		},
		{
			Value:       appPty.CodexSandboxReadOnly,
			Label:       "Read Only",
			Description: "Read-only exploration. Codex can inspect the worktree but cannot change it.",
		},
		{
			Value:       appPty.CodexSandboxFullAccess,
			Label:       "Full Access",
			Description: "No sandbox at all. Combined with approvals off, nothing stands between Codex and the machine.",
		},
	}
}

// defaultCodexSandbox falls back to Codex's own default policy.
func defaultCodexSandbox(last string) string {
	switch last {
	case appPty.CodexSandboxReadOnly, appPty.CodexSandboxWorkspace, appPty.CodexSandboxFullAccess:
		return last
	}
	return appPty.CodexSandboxWorkspace
}

// codexApprovalValue maps the dialog's "Ask for approval" checkbox onto the
// policy codex --ask-for-approval takes.
func codexApprovalValue(ask bool) string {
	if ask {
		return appPty.CodexApprovalOnRequest
	}
	return appPty.CodexApprovalNever
}

// codexApprovalAsks is codexApprovalValue's inverse, for restoring the
// checkbox from a persisted policy. Anything but an explicit "never" reads as
// asking, so an unknown value errs toward prompting.
func codexApprovalAsks(policy string) bool {
	return policy != appPty.CodexApprovalNever
}
