package vterm

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const maxOSCPayload = 1024 * 1024

// ShellMarker records the latest OSC 133 shell-integration boundary.
type ShellMarker struct {
	Kind       string
	Parameters []string
	CursorX    int
	CursorY    int
}

func (v *VTerm) initOSCPalette() {
	base := [...]uint32{
		0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080, 0x008080, 0xc0c0c0,
		0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff,
	}
	for i, rgb := range base {
		v.Palette[i] = Color{Type: ColorRGB, Value: rgb}
	}
	for i := 16; i < 232; i++ {
		n := i - 16
		r, g, b := n/36, (n/6)%6, n%6
		component := func(x int) uint32 {
			if x == 0 {
				return 0
			}
			return uint32(55 + 40*x)
		}
		v.Palette[i] = Color{Type: ColorRGB, Value: component(r)<<16 | component(g)<<8 | component(b)}
	}
	for i := 232; i < 256; i++ {
		gray := uint32(8 + 10*(i-232))
		v.Palette[i] = Color{Type: ColorRGB, Value: gray<<16 | gray<<8 | gray}
	}
	if v.DefaultForeground.Type == ColorDefault {
		v.DefaultForeground = Color{Type: ColorRGB, Value: 0xc0c0c0}
	}
	if v.DefaultBackground.Type == ColorDefault {
		v.DefaultBackground = Color{Type: ColorRGB, Value: 0x000000}
	}
	if v.CursorColor.Type == ColorDefault {
		v.CursorColor = v.DefaultForeground
	}
}

func (p *Parser) executeOSC(stTerminated bool) {
	data := p.oscBuf.String()
	p.oscBuf.Reset()
	p.oscOverflow = false

	code, rest, ok := strings.Cut(data, ";")
	if !ok {
		rest = ""
	}
	switch code {
	case "0":
		p.vt.IconName, p.vt.Title = rest, rest
	case "1":
		p.vt.IconName = rest
	case "2":
		p.vt.Title = rest
	case "4":
		p.executeOSCPalette(rest, stTerminated)
	case "7":
		p.vt.WorkingDirectory = oscWorkingDirectory(rest)
	case "8":
		params, uri, found := strings.Cut(rest, ";")
		if found {
			p.vt.setHyperlink(uri, params)
		}
	case "10", "11", "12":
		p.executeOSCDynamicColor(code, rest, stTerminated)
	case "133":
		parts := strings.Split(rest, ";")
		if len(parts) > 0 {
			p.vt.ShellMarker = ShellMarker{Kind: parts[0], Parameters: append([]string(nil), parts[1:]...), CursorX: p.vt.CursorX, CursorY: p.vt.CursorY}
		}
	}
}

func (p *Parser) executeOSCPalette(rest string, st bool) {
	parts := strings.Split(rest, ";")
	for i := 0; i+1 < len(parts); i += 2 {
		idx, err := strconv.Atoi(parts[i])
		if err != nil || idx < 0 || idx >= len(p.vt.Palette) {
			continue
		}
		if parts[i+1] == "?" {
			p.vt.respondOSC(fmt.Sprintf("4;%d;%s", idx, oscColor(p.vt.Palette[idx])), st)
		} else if c, ok := parseOSCColor(parts[i+1]); ok {
			p.vt.replaceIndexedColor(uint32(idx), c)
			p.vt.Palette[idx] = c
			p.vt.PaletteModified[idx] = true
		}
	}
}

func (v *VTerm) replaceIndexedColor(index uint32, replacement Color) {
	replace := func(style *Style) {
		if style.Fg.Type == ColorIndexed && style.Fg.Value == index {
			style.Fg = replacement
		}
		if style.Bg.Type == ColorIndexed && style.Bg.Value == index {
			style.Bg = replacement
		}
	}
	for _, screen := range [][][]Cell{v.Screen, v.Scrollback, v.altScreenBuf, v.syncScreen} {
		for y := range screen {
			for x := range screen[y] {
				replace(&screen[y][x].Style)
			}
		}
	}
	replace(&v.CurrentStyle)
	replace(&v.SavedStyle)
	v.invalidateRenderCache()
}

func (v *VTerm) indexedColor(index uint32) Color {
	if index < 256 && v.PaletteModified[index] {
		return v.Palette[index]
	}
	return Color{Type: ColorIndexed, Value: index}
}

func (p *Parser) executeOSCDynamicColor(code, value string, st bool) {
	target := &p.vt.DefaultForeground
	if code == "11" {
		target = &p.vt.DefaultBackground
	}
	if code == "12" {
		target = &p.vt.CursorColor
	}
	if value == "?" {
		p.vt.respondOSC(code+";"+oscColor(*target), st)
	} else if c, ok := parseOSCColor(value); ok {
		*target = c
		if code == "10" {
			p.vt.defaultForegroundModified = true
		}
		if code == "11" {
			p.vt.defaultBackgroundModified = true
		}
		if code == "12" {
			p.vt.cursorColorModified = true
		}
	}
}

func (v *VTerm) respondOSC(payload string, st bool) {
	term := "\a"
	if st {
		term = "\x1b\\"
	}
	v.respond([]byte("\x1b]" + payload + term))
}

func oscWorkingDirectory(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		return raw
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return u.Path
	}
	return path
}

func oscColor(c Color) string {
	if c.Type == ColorIndexed && int(c.Value) < 256 {
		return fmt.Sprintf("rgb:%04x/%04x/%04x", c.Value, c.Value, c.Value)
	}
	r := (c.Value >> 16) & 0xff
	g := (c.Value >> 8) & 0xff
	b := c.Value & 0xff
	return fmt.Sprintf("rgb:%02x%02x/%02x%02x/%02x%02x", r, r, g, g, b, b)
}

func parseOSCColor(s string) (Color, bool) {
	if strings.HasPrefix(s, "#") && len(s) == 7 {
		v, err := strconv.ParseUint(s[1:], 16, 32)
		return Color{Type: ColorRGB, Value: uint32(v)}, err == nil
	}
	if !strings.HasPrefix(s, "rgb:") {
		return Color{}, false
	}
	parts := strings.Split(strings.TrimPrefix(s, "rgb:"), "/")
	if len(parts) != 3 {
		return Color{}, false
	}
	var rgb uint32
	for _, part := range parts {
		if len(part) < 1 || len(part) > 4 {
			return Color{}, false
		}
		v, err := strconv.ParseUint(part, 16, 16)
		if err != nil {
			return Color{}, false
		}
		max := uint64(1)<<(4*len(part)) - 1
		rgb = rgb<<8 | uint32((v*255+max/2)/max)
	}
	return Color{Type: ColorRGB, Value: rgb}, true
}
