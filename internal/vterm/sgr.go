package vterm

// executeSGR applies one SGR sequence to the current style.
//
// Parameters arrive in two shapes. Semicolon-separated ones are independent
// ("1;4;31" is bold, underline, red) apart from the extended colors, which
// consume the parameters after them ("38;2;r;g;b"). Colon-separated ones are
// sub-parameters of the parameter they follow ("4:3" is a curly underline,
// "38:2::r:g:b" a truecolor foreground), and must never be read as parameters
// in their own right: "4:3" read flat is underline plus italic, and the empty
// fields tmux sends in an underline color ("58::2::r::g::b") read flat are a
// string of full resets that wipe the colors set just before them.
func (p *Parser) executeSGR() {
	if len(p.params) == 0 {
		p.params = append(p.params, 0)
		p.paramKinds = append(p.paramKinds, paramMain)
	}

	for i := 0; i < len(p.params); i++ {
		end := i + 1
		for end < len(p.params) && p.subParam(end) {
			end++
		}
		if end > i+1 {
			p.applySGRWithSubParams(p.params[i], i+1, end)
			i = end - 1
			continue
		}
		i = p.applySGR(i)
	}
}

// subParam reports whether params[i] is a colon sub-parameter.
func (p *Parser) subParam(i int) bool {
	return i < len(p.paramKinds) && p.paramKinds[i] != paramMain
}

// applySGR applies the semicolon-form parameter at params[i] and returns the
// index of the last parameter it consumed.
func (p *Parser) applySGR(i int) int {
	style := &p.vt.CurrentStyle
	param := p.params[i]
	switch param {
	case 0: // Reset
		*style = Style{}
	case 1:
		style.Bold = true
	case 2:
		style.Dim = true
	case 3:
		style.Italic = true
	case 4:
		style.Underline = UnderlineSingle
	case 5, 6:
		style.Blink = true
	case 7:
		style.Reverse = true
	case 8:
		style.Hidden = true
	case 9:
		style.Strike = true
	case 21:
		style.Bold = false
	case 22:
		style.Bold = false
		style.Dim = false
	case 23:
		style.Italic = false
	case 24:
		style.Underline = UnderlineNone
	case 25:
		style.Blink = false
	case 27:
		style.Reverse = false
	case 28:
		style.Hidden = false
	case 29:
		style.Strike = false
	case 30, 31, 32, 33, 34, 35, 36, 37: // FG colors 0-7
		style.Fg = p.vt.indexedColor(uint32(param - 30))
	case 38: // Extended FG
		return p.parseExtendedColor(i, &style.Fg)
	case 39: // Default FG
		style.Fg = Color{Type: ColorDefault}
	case 40, 41, 42, 43, 44, 45, 46, 47: // BG colors 0-7
		style.Bg = p.vt.indexedColor(uint32(param - 40))
	case 48: // Extended BG
		return p.parseExtendedColor(i, &style.Bg)
	case 49: // Default BG
		style.Bg = Color{Type: ColorDefault}
	case 58: // Underline color: parsed so its operands are not misread, then dropped
		var discard Color
		return p.parseExtendedColor(i, &discard)
	case 90, 91, 92, 93, 94, 95, 96, 97: // Bright FG
		style.Fg = p.vt.indexedColor(uint32(param - 90 + 8))
	case 100, 101, 102, 103, 104, 105, 106, 107: // Bright BG
		style.Bg = p.vt.indexedColor(uint32(param - 100 + 8))
	}
	return i
}

// applySGRWithSubParams applies a parameter that carries colon
// sub-parameters, held in params[from:to].
func (p *Parser) applySGRWithSubParams(param, from, to int) {
	style := &p.vt.CurrentStyle
	switch param {
	case 4:
		// tmux's own usstyle capability writes "4::3", with an empty field
		// before the style; the first non-empty field is the style.
		style.Underline = underlineFromSGR(p.firstSubParam(from, to))
	case 38:
		if c, ok := p.colonColor(from, to); ok {
			style.Fg = c
		}
	case 48:
		if c, ok := p.colonColor(from, to); ok {
			style.Bg = c
		}
	case 58:
		// Underline color is recognised and dropped: a cell has no room for
		// a third color, and no agent has been seen emitting one.
	default:
		// Sub-parameters are meaningless to anything else; apply the
		// parameter itself.
		p.applySGR(from - 1)
	}
}

// firstSubParam returns the first non-empty sub-parameter in
// params[from:to], or 0 when every field is empty.
func (p *Parser) firstSubParam(from, to int) int {
	for j := from; j < to; j++ {
		if p.paramKinds[j] != paramSubEmpty {
			return p.params[j]
		}
	}
	return 0
}

// underlineFromSGR maps the SGR 4 sub-parameter to an underline style. An
// unknown style still underlines, which is what the agent asked for.
func underlineFromSGR(n int) UnderlineStyle {
	switch {
	case n <= 0:
		return UnderlineNone
	case n <= int(UnderlineDashed):
		return UnderlineStyle(n)
	default:
		return UnderlineSingle
	}
}

// colonColor decodes the operands of a colon-form extended color. The fields
// that may be empty (the color-space id in "38:2::r:g:b", and every gap in the
// "58::2::r::g::b" form iTerm2's terminfo hands tmux) carry no value, so only
// the non-empty ones are read: 2 then r, g, b, with an optional color-space id
// before them; or 5 then a palette index.
func (p *Parser) colonColor(from, to int) (Color, bool) {
	var vals [6]int
	n := 0
	for j := from; j < to && n < len(vals); j++ {
		if p.paramKinds[j] == paramSubEmpty {
			continue
		}
		vals[n] = p.params[j]
		n++
	}
	switch {
	case n >= 5 && vals[0] == 2:
		return rgbColor(vals[2], vals[3], vals[4]), true
	case n == 4 && vals[0] == 2:
		return rgbColor(vals[1], vals[2], vals[3]), true
	case n >= 2 && vals[0] == 5:
		return p.vt.indexedColor(uint32(clampByte(vals[1]))), true
	}
	return Color{}, false
}

func (p *Parser) parseExtendedColor(i int, color *Color) int {
	if i+1 >= len(p.params) {
		return i
	}

	switch p.params[i+1] {
	case 2: // RGB
		if i+4 < len(p.params) {
			*color = rgbColor(p.params[i+2], p.params[i+3], p.params[i+4])
			return i + 4
		}
	case 5: // 256 color
		if i+2 < len(p.params) {
			*color = p.vt.indexedColor(uint32(clampByte(p.params[i+2])))
			return i + 2
		}
	}
	return i + 1
}

func rgbColor(r, g, b int) Color {
	return Color{Type: ColorRGB, Value: uint32(clampByte(r))<<16 | uint32(clampByte(g))<<8 | uint32(clampByte(b))}
}

func clampByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
