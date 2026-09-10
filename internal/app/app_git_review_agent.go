package app

import (
	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/gitreview"
	appPty "github.com/Skowt/medusa/internal/pty"
)

// Display names for the assistants a review can be aimed at.
const (
	claudeLabel = "Claude"
	codexLabel  = "Codex"
)

// reviewAgentProfile describes what the review page may promise about an agent.
//
// The interesting case is Codex. Its sandbox decides whether it can reach the
// review server at all, and on Medusa's default policy it cannot: under
// `workspace-write` Seatbelt denies network, and a denied connection surfaces as
// an ordinary "couldn't connect" rather than a permission error. Left
// undetected, the agent reports the review page as broken and the user goes
// looking for a server that is running perfectly well — so the page says up
// front that replies are unavailable, and how to turn them on.
//
// Claude is treated as reply-capable. That is right for every tab in practice,
// though a tab launched with the Sandboxed toggle may well be subject to the
// same restriction; what Claude Code's own sandbox does to loopback traffic has
// not been established here, and claiming either way without knowing would be
// worse than the status quo. Adding it later is a matter of one more case.
func reviewAgentProfile(cfg *config.Config, ws *data.Workspace, assistant, codexSandbox string) gitreview.AgentProfile {
	if assistant != assistantCodex {
		return gitreview.AgentProfile{Label: claudeLabel, CanReply: true}
	}

	profile := gitreview.AgentProfile{Label: codexLabel}
	switch codexSandbox {
	case appPty.CodexSandboxFullAccess:
		profile.CanReply = true
		return profile
	case appPty.CodexSandboxReadOnly:
		profile.Blocked = "This Codex tab runs with the Read Only sandbox, which has no network " +
			"access, so it cannot reach this page to reply."
		profile.Fix = "Start a Codex tab with the Full Access sandbox to enable replies."
		return profile
	}

	// workspace-write, and anything unrecognised: assume the default policy,
	// which is the one that blocks network. Guessing the other way would leave
	// the user waiting for replies that can never arrive.
	codexHome := ""
	if ws != nil {
		codexHome = config.CodexHomeDir(cfg.Paths.ProfilesRoot, ws.Profile)
	}
	if config.CodexNetworkAccessEnabled(codexHome) {
		profile.CanReply = true
		return profile
	}

	profile.Blocked = "This Codex tab runs with the Workspace Write sandbox, which blocks " +
		"network access, so Codex cannot reach this page to reply to comments or resolve them. " +
		"Your comments still reach it and it will still make the changes."
	profile.Fix = codexNetworkFix(codexHome)
	return profile
}

// codexNetworkFix explains how to let Codex reach the review server.
//
// It names the profile's real config.toml rather than ~/.codex/config.toml,
// which is the file the user would otherwise reach for and the one Codex would
// ignore: Medusa points CODEX_HOME at the profile directory, so that is the
// config any Medusa-launched Codex tab actually reads.
func codexNetworkFix(codexHome string) string {
	fix := "Either start the Codex tab with the Full Access sandbox, or add this to "
	if codexHome == "" {
		fix += "your profile's Codex config.toml"
	} else {
		fix += codexHome + "/config.toml"
	}
	return fix + ":\n\n[sandbox_workspace_write]\nnetwork_access = true\n\n" +
		"then restart the tab. Note that this grants network access generally, not just to this page."
}
