package vterm

import "fmt"

// Color-scheme reporting (DECRQM-style "CSI ? 996 n", replied to with
// "CSI ? 997 ; 1 n" for dark or "; 2 n" for light, and DEC mode 2031 for
// unsolicited reports when the scheme changes).
//
// tmux asks its client which scheme it is in at attach, and relays the answer
// to any pane that enables 2031 -- which Claude Code does at startup, so its
// "Auto (match terminal)" theme can follow the terminal. A center tab's vterm
// *is* that client, and it never answered: tmux could not tell the agent
// anything, so the agent's theme could not follow medusa's.
//
// The scheme is read from the default background, the same color the vterm
// reports for an OSC 11 query. medusa paints that color as the outer
// terminal's background (View.BackgroundColor), so it is what is really behind
// the agent.

const (
	colorSchemeDark  = 1
	colorSchemeLight = 2
)

// ResetClientModes forgets the modes a terminal client turned on: focus
// reporting (1004) and colour-scheme reports (2031). A reattach reuses the
// tab's vterm for a new tmux client, which enables them afresh at attach;
// until it has, the old client's flags would claim it already had.
func (v *VTerm) ResetClientModes() {
	v.focusReporting = false
	v.colorSchemeUpdates = false
}

func (v *VTerm) colorScheme() int {
	if isLightColor(v.DefaultBackground) {
		return colorSchemeLight
	}
	return colorSchemeDark
}

func (v *VTerm) reportColorScheme() {
	v.respond([]byte(fmt.Sprintf("\x1b[?997;%dn", v.colorScheme())))
}

// isLightColor reports whether a background is light, by perceived luminance.
// Anything not RGB is the terminal's own default, which medusa sets dark
// unless a theme says otherwise.
func isLightColor(c Color) bool {
	if c.Type != ColorRGB {
		return false
	}
	r := float64((c.Value >> 16) & 0xff)
	g := float64((c.Value >> 8) & 0xff)
	b := float64(c.Value & 0xff)
	return 0.299*r+0.587*g+0.114*b >= 128
}
