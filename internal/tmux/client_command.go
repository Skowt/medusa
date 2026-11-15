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

	settings := sessionSettings(base, session, opts, tags)

	// Use attach -d to detach other clients (handles multi-instance gracefully)
	attach := fmt.Sprintf("%s attach -dt %s", base, session)

	return fmt.Sprintf("%s && %s%s", create, settings, attach)
}

// sessionSettings returns the set-option commands that make a session behave
// as a medusa tab, each terminated by "; ". It is shared by the attaching
// client command and by StartDetachedSession, so a session started by the CLI
// is indistinguishable from one the TUI started.
func sessionSettings(base, session string, opts Options, tags SessionTags) string {
	var settings strings.Builder
	// Disable tmux prefix for this session only (not global) to make it transparent
	fmt.Fprintf(&settings, "%s set-option -t %s prefix None 2>/dev/null; ", base, session)
	fmt.Fprintf(&settings, "%s set-option -t %s prefix2 None 2>/dev/null; ", base, session)
	if opts.HideStatus {
		fmt.Fprintf(&settings, "%s set-option -t %s status off 2>/dev/null; ", base, session)
	}
	if tags.Fullscreen {
		// Fullscreen Claude owns the mouse; tmux must forward events to it.
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
	appendClientFeatures(&settings, base)
	appendSessionTags(&settings, base, session, tags)

	return settings.String()
}

// clientFeatures is what medusa's vterm -- the terminal this client really
// is -- can draw, declared to tmux so it forwards each one instead of dropping
// or downgrading it:
//
//   - hyperlinks: OSC 8. Without it tmux strips the URI and passes on only the
//     link text, leaving the outer terminal to guess a URL from that text (an
//     MR link like services/protos!1638 became http://services/protos!1638).
//   - RGB: 24-bit color. Without it tmux quantises truecolor to the 256-color
//     cube; tmux grants it only when the client's own environment happens to
//     carry COLORTERM=truecolor.
//   - usstyle: curly, dotted, dashed and double underlines.
//   - strikethrough.
//
// tmux keys these by terminal name and learns nothing about medusa's vterm
// from terminfo: TERM is xterm-256color (ClientTerm), whose entry says nothing
// about any of them, and whatever tmux did find came from the outer terminal's
// terminfo through an inherited TERMINFO_DIRS -- iTerm2's, say, which describes
// iTerm2 rather than the vterm actually parsing the bytes.
const clientFeatures = ClientTerm + ":hyperlinks:RGB:usstyle:strikethrough"

// clientFeaturesIndex is the terminal-features slot medusa owns. Writing a
// fixed slot is idempotent, which an append is not: this runs on every attach
// against a server-wide array, and a check-then-append raced when several tabs
// attached at once, leaving duplicate entries behind.
const clientFeaturesIndex = 77

func appendClientFeatures(settings *strings.Builder, base string) {
	fmt.Fprintf(settings, "%s set-option -s %s %s 2>/dev/null; ",
		base, shellutil.Quote(fmt.Sprintf("terminal-features[%d]", clientFeaturesIndex)), shellutil.Quote(clientFeatures))
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
