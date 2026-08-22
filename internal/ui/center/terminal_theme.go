package center

import (
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/vterm"
)

func newThemedVTerm(width, height int) *vterm.VTerm {
	term := vterm.New(width, height)
	applyTerminalTheme(term)
	return term
}

func applyTerminalTheme(term *vterm.VTerm) {
	if term == nil {
		return
	}
	term.SetDefaultColors(
		vterm.ColorFromGoColor(common.ColorForeground),
		vterm.ColorFromGoColor(common.ColorBackground),
		vterm.ColorFromGoColor(common.ColorForeground),
	)
}
