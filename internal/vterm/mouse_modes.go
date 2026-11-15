package vterm

// Mouse reporting modes the application can enable via DECSET.
const (
	mouseModeNormal    uint8 = 1 << iota // DECSET 1000: press/release
	mouseModeButton                      // DECSET 1002: press/release + drag
	mouseModeAnyMotion                   // DECSET 1003: all motion
)

func (v *VTerm) setMouseMode(mode uint8, on bool) {
	if on {
		v.mouseModes |= mode
	} else {
		v.mouseModes &^= mode
	}
}

// MouseReporting reports whether the application has enabled any xterm mouse
// reporting mode (DECSET 1000/1002/1003).
func (v *VTerm) MouseReporting() bool {
	return v.mouseModes != 0
}

// FocusReporting reports whether the application enabled focus reporting (DEC
// mode 1004). For a center tab the application is the tmux client, which
// enables it at attach when the server has focus-events on.
func (v *VTerm) FocusReporting() bool {
	return v.focusReporting
}
