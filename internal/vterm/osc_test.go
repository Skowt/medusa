package vterm

import (
	"strings"
	"testing"
)

func TestOSCTitlesAndWorkingDirectory(t *testing.T) {
	vt := New(40, 4)
	vt.Write([]byte("\x1b]0;both\a\x1b]1;icon\x1b\\\x1b]2;title\a\x1b]7;file://host/tmp/a%20b\x1b\\"))
	if vt.IconName != "icon" || vt.Title != "title" {
		t.Fatalf("titles = %q, %q", vt.IconName, vt.Title)
	}
	if vt.WorkingDirectory != "/tmp/a b" {
		t.Fatalf("cwd = %q", vt.WorkingDirectory)
	}
}

func TestOSCDynamicColorQueriesUseMatchingTerminator(t *testing.T) {
	vt := New(20, 2)
	vt.SetDefaultColors(Color{Type: ColorRGB, Value: 0x112233}, Color{Type: ColorRGB, Value: 0x445566}, Color{Type: ColorRGB, Value: 0x778899})
	var replies strings.Builder
	vt.SetResponseWriter(func(data []byte) { replies.Write(data) })
	vt.Write([]byte("\x1b]10;?\a\x1b]11;?\x1b\\\x1b]12;?\a"))
	want := "\x1b]10;rgb:1111/2222/3333\a\x1b]11;rgb:4444/5555/6666\x1b\\\x1b]12;rgb:7777/8888/9999\a"
	if replies.String() != want {
		t.Fatalf("replies = %q, want %q", replies.String(), want)
	}
}

func TestOSCPaletteQuerySetAndRendering(t *testing.T) {
	vt := New(20, 2)
	var reply string
	vt.SetResponseWriter(func(data []byte) { reply += string(data) })
	vt.Write([]byte("\x1b[31mA\x1b]4;1;#123456\a\x1b[31mB\x1b]4;1;?\a"))
	if reply != "\x1b]4;1;rgb:1212/3434/5656\a" {
		t.Fatalf("reply = %q", reply)
	}
	for i := 0; i < 2; i++ {
		got := vt.Screen[0][i].Style.Fg
		if got.Type != ColorRGB || got.Value != 0x123456 {
			t.Fatalf("cell %d color = %#v", i, got)
		}
	}
}

func TestOSC133RecordsShellBoundary(t *testing.T) {
	vt := New(20, 2)
	vt.Write([]byte("abc\x1b]133;D;7\x1b\\"))
	if vt.ShellMarker.Kind != "D" || len(vt.ShellMarker.Parameters) != 1 || vt.ShellMarker.Parameters[0] != "7" || vt.ShellMarker.CursorX != 3 {
		t.Fatalf("marker = %#v", vt.ShellMarker)
	}
}

func TestOversizedOSCIsDiscardedAndParserRecovers(t *testing.T) {
	vt := New(20, 2)
	vt.Write([]byte("\x1b]2;" + strings.Repeat("x", maxOSCPayload+1) + "\aOK"))
	if vt.Title != "" {
		t.Fatal("oversized title was accepted")
	}
	if got := string([]rune{vt.Screen[0][0].Rune, vt.Screen[0][1].Rune}); got != "OK" {
		t.Fatalf("screen starts %q", got)
	}
}
