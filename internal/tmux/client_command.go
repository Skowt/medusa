package tmux

import (
	"fmt"
	"strings"

	"github.com/Skowt/medusa/internal/shellutil"
)

// ClientTerm is the TERM medusa's tmux client attaches with; it must match the
// TERM set in internal/pty.NewWithSize, since terminal-features below is keyed
// by terminal name.
const ClientTerm = "xterm-256color"

func ClientCommand(sessionName, workDir, command string) string {
	return ClientCommandWithOptions(sessionName, workDir, command, DefaultOptions())
}

func ClientCommandWithOptions(sessionName, workDir, command string, opts Options) string {
	return clientCommand(sessionName, workDir, command, opts, SessionTags{})
}

func ClientCommandWithTags(sessionName, workDir, command string, opts Options, tags SessionTags) string {
	return clientCommand(sessionName, workDir, command, opts, tags)
}

func clientCommand(sessionName, workDir, command string, opts Options, tags SessionTags) string {
	base := tmuxBase(opts)
	session := shellutil.Quote(sessionName)
	dir := shellutil.Quote(workDir)
	cmd := shellutil.Quote(command)

	// Use atomic new-session -Ad: creates if missing, attaches if exists (detaching other clients)
	create := fmt.Sprintf("%s new-session -Ads %s -c %s sh -lc %s",
		base, session, dir, cmd)

	var settings strings.Builder
	// Disable tmux prefix for this session only (not global) to make it transparent
	fmt.Fprintf(&settings, "%s set-option -t %s prefix None 2>/dev/null; ", base, session)
	fmt.Fprintf(&settings, "%s set-option -t %s prefix2 None 2>/dev/null; ", base, session)
	if opts.HideStatus {
		fmt.Fprintf(&settings, "%s set-option -t %s status off 2>/dev/null; ", base, session)
	}
	if tags.Fullscreen {
		// Fullscreen Claude owns the mouse; tmux must forward mouse events to it.
		fmt.Fprintf(&settings, "%s set-option -t %s mouse on 2>/dev/null; ", base, session)
	} else if opts.DisableMouse {
		fmt.Fprintf(&settings, "%s set-option -t %s mouse off 2>/dev/null; ", base, session)
	}
	if opts.DefaultTerminal != "" {
		fmt.Fprintf(&settings, "%s set-option -t %s default-terminal %s 2>/dev/null; ", base, session, shellutil.Quote(opts.DefaultTerminal))
	}
	// Ensure activity timestamps update for window_activity-based tracking.
	fmt.Fprintf(&settings, "%s set-option -t %s -w monitor-activity on 2>/dev/null; ", base, session)
	// Server option: forward terminal focus events so agents (e.g. Claude Code) can track focus.
	fmt.Fprintf(&settings, "%s set-option -s focus-events on 2>/dev/null; ", base)
	appendHyperlinkFeature(&settings, base)
	appendSessionTags(&settings, base, session, tags)

	// Use attach -d to detach other clients (handles multi-instance gracefully)
	attach := fmt.Sprintf("%s attach -dt %s", base, session)

	return fmt.Sprintf("%s && %s%s", create, settings.String(), attach)
}

// appendHyperlinkFeature tells tmux that medusa's client understands OSC 8.
//
// tmux only forwards a pane's hyperlinks to clients whose terminal advertises
// the "hyperlinks" feature. We attach with TERM=xterm-256color, whose terminfo
// says nothing about hyperlinks, so tmux strips the URI and passes on only the
// link text — leaving the outer terminal to guess a URL from that text (an MR
// link like services/protos!1638 becomes http://services/protos!1638).
//
// terminal-features is a server-wide array and this runs on every attach, so a
// bare append would add a duplicate entry per tab; only append when absent.
func appendHyperlinkFeature(settings *strings.Builder, base string) {
	feature := ClientTerm + ":hyperlinks"
	fmt.Fprintf(settings, "%s show-options -s terminal-features 2>/dev/null | grep -qF %s || %s set-option -sa terminal-features %s 2>/dev/null; ",
		base, shellutil.Quote(feature), base, shellutil.Quote(","+feature))
}

func appendSessionTags(settings *strings.Builder, base, session string, tags SessionTags) {
	if tags.WorkspaceID == "" && tags.TabID == "" && tags.Type == "" && tags.Assistant == "" && tags.CreatedAt == 0 && !tags.Fullscreen {
		return
	}
	fmt.Fprintf(settings, "%s set-option -t %s @medusa 1 2>/dev/null; ", base, session)
	if tags.WorkspaceID != "" {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_workspace %s 2>/dev/null; ", base, session, shellutil.Quote(tags.WorkspaceID))
	}
	if tags.TabID != "" {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_tab %s 2>/dev/null; ", base, session, shellutil.Quote(tags.TabID))
	}
	if tags.Type != "" {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_type %s 2>/dev/null; ", base, session, shellutil.Quote(tags.Type))
	}
	if tags.Assistant != "" {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_assistant %s 2>/dev/null; ", base, session, shellutil.Quote(tags.Assistant))
	}
	if tags.CreatedAt != 0 {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_created_at %s 2>/dev/null; ", base, session, shellutil.Quote(fmt.Sprintf("%d", tags.CreatedAt)))
	}
	if tags.Fullscreen {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_fullscreen 1 2>/dev/null; ", base, session)
	}
}

func tmuxBase(opts Options) string {
	base := "tmux"
	if opts.ServerName != "" {
		base = fmt.Sprintf("%s -L %s", base, shellutil.Quote(opts.ServerName))
	}
	if opts.ConfigPath != "" {
		base = fmt.Sprintf("%s -f %s", base, shellutil.Quote(opts.ConfigPath))
	}
	return base
}
