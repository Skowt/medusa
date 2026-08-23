package vterm

import "testing"

func TestEraseUsesCurrentBackground(t *testing.T) {
	bg := Color{Type: ColorRGB, Value: 0x41454c}

	tests := []struct {
		name  string
		write string
		check func(*VTerm) []Cell
	}{
		{
			name:  "erase display",
			write: "\x1b[48;2;65;69;76m\x1b[2J",
			check: func(v *VTerm) []Cell { return append(v.Screen[0], v.Screen[1]...) },
		},
		{
			name:  "erase line",
			write: "\x1b[48;2;65;69;76m\x1b[2K",
			check: func(v *VTerm) []Cell { return v.Screen[0] },
		},
		{
			name:  "erase characters",
			write: "abc\r\x1b[48;2;65;69;76m\x1b[3X",
			check: func(v *VTerm) []Cell { return v.Screen[0][:3] },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vt := New(4, 2)
			vt.Write([]byte(tt.write))
			for i, cell := range tt.check(vt) {
				if cell.Rune != ' ' || cell.Width != 1 {
					t.Fatalf("cell %d = %+v, want a normal blank cell", i, cell)
				}
				if cell.Style != (Style{Bg: bg}) {
					t.Fatalf("cell %d style = %+v, want only background %+v", i, cell.Style, bg)
				}
			}
		})
	}
}

func TestEraseAfterBackgroundResetUsesDefaultCell(t *testing.T) {
	vt := New(3, 1)
	vt.Write([]byte("\x1b[48;2;65;69;76m\x1b[49m\x1b[2K"))

	for i, cell := range vt.Screen[0] {
		if cell != DefaultCell() {
			t.Fatalf("cell %d = %+v, want default cell", i, cell)
		}
	}
}
