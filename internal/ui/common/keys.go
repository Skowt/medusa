package common

import (
	"strconv"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// KeyToBytes converts a key press message to the bytes a real terminal would
// have sent for it. Dropping a modifier here fails silently: flattening
// shift+enter to a bare CR submits the prompt the user meant to break onto a
// new line. See CLAUDE.md, "Key forwarding".
func KeyToBytes(msg tea.KeyPressMsg) []byte {
	key := msg.Key()
	alt := key.Mod&tea.ModAlt != 0
	ctrl := key.Mod&tea.ModCtrl != 0
	shift := key.Mod&tea.ModShift != 0

	switch key.Code {
	case tea.KeyEnter:
		// ESC CR is meta+enter, which Claude Code inserts as a newline. The
		// Kitty CSI 13;2u form shift+enter actually arrives as would be
		// stripped by tmux, since medusa never enables extended-keys.
		if alt || shift {
			return []byte{0x1b, '\r'}
		}
		return []byte{'\r'}
	case tea.KeyBackspace:
		// ESC DEL is readline's delete-word-backwards.
		if alt || ctrl {
			return []byte{0x1b, 0x7f}
		}
		return []byte{0x7f}
	case tea.KeyTab:
		switch {
		case shift:
			return []byte{0x1b, '[', 'Z'}
		case alt:
			return []byte{0x1b, '\t'}
		}
		return []byte{'\t'}
	case tea.KeySpace:
		switch {
		case ctrl:
			return []byte{0x00}
		case alt:
			return []byte{0x1b, ' '}
		}
		return []byte{' '}
	case tea.KeyEscape:
		return []byte{0x1b}
	case tea.KeyUp:
		return cursorKey('A', key.Mod)
	case tea.KeyDown:
		return cursorKey('B', key.Mod)
	case tea.KeyRight:
		return cursorKey('C', key.Mod)
	case tea.KeyLeft:
		return cursorKey('D', key.Mod)
	case tea.KeyHome:
		return cursorKey('H', key.Mod)
	case tea.KeyEnd:
		return cursorKey('F', key.Mod)
	case tea.KeyInsert:
		return tildeKey(2, key.Mod)
	case tea.KeyDelete:
		return tildeKey(3, key.Mod)
	case tea.KeyPgUp:
		return tildeKey(5, key.Mod)
	case tea.KeyPgDown:
		return tildeKey(6, key.Mod)
	}

	if ctrl {
		if b, ok := ctrlByte(key.Code); ok {
			if alt {
				return []byte{0x1b, b}
			}
			return []byte{b}
		}
	}

	if alt {
		if r := typedRune(key); r != 0 {
			return append([]byte{0x1b}, []byte(string(r))...)
		}
		return nil
	}

	if key.Text != "" {
		return []byte(key.Text)
	}

	if s := msg.String(); len(s) == 1 {
		return []byte(s)
	}

	return nil
}

// ctrlByte maps a key held with ctrl to its C0 control byte. Letters are
// arithmetic rather than a table so none can be missing.
func ctrlByte(code rune) (byte, bool) {
	switch {
	case code >= 'a' && code <= 'z':
		return byte(code-'a') + 1, true
	case code >= 'A' && code <= 'Z':
		return byte(code-'A') + 1, true
	case code == '@':
		return 0x00, true
	case code == '[':
		return 0x1b, true
	case code == '\\':
		return 0x1c, true
	case code == ']':
		return 0x1d, true
	case code == '^':
		return 0x1e, true
	case code == '_', code == '/':
		return 0x1f, true
	case code == '?':
		return 0x7f, true
	}
	return 0, false
}

// typedRune returns the character a modified key would have typed. Both
// decoders clear Key.Text for anything held with more than shift, so alt+<char>
// has to be rebuilt from the key code or it never reaches the agent.
func typedRune(key tea.Key) rune {
	if key.Text != "" {
		return []rune(key.Text)[0]
	}
	r := key.Code
	if key.Mod&tea.ModShift != 0 {
		if key.ShiftedCode != 0 {
			r = key.ShiftedCode
		} else {
			r = unicode.ToUpper(r)
		}
	}
	if r > unicode.MaxRune || !unicode.IsPrint(r) {
		return 0
	}
	return r
}

// csiModifier returns the xterm modifier parameter for mod, or 0 when none
// applies. Super/Meta are excluded: xterm has no encoding for them, and medusa
// binds Cmd itself (Cmd+C copies the selection).
func csiModifier(mod tea.KeyMod) int {
	param := 0
	if mod&tea.ModShift != 0 {
		param |= 1
	}
	if mod&tea.ModAlt != 0 {
		param |= 2
	}
	if mod&tea.ModCtrl != 0 {
		param |= 4
	}
	if param == 0 {
		return 0
	}
	return param + 1
}

// cursorKey encodes an arrow/Home/End key as CSI <final>, or CSI 1;<mod><final>
// when modifiers apply.
func cursorKey(final byte, mod tea.KeyMod) []byte {
	if param := csiModifier(mod); param != 0 {
		return append([]byte("\x1b[1;"+strconv.Itoa(param)), final)
	}
	return []byte{0x1b, '[', final}
}

// tildeKey encodes Insert/Delete/PgUp/PgDown as CSI <num>~ or CSI <num>;<mod>~.
func tildeKey(num int, mod tea.KeyMod) []byte {
	if param := csiModifier(mod); param != 0 {
		return []byte("\x1b[" + strconv.Itoa(num) + ";" + strconv.Itoa(param) + "~")
	}
	return []byte("\x1b[" + strconv.Itoa(num) + "~")
}
