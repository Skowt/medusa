package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/ide"
)

func sampleInstalls() []ide.Install {
	return []ide.Install{
		{Name: "Cursor", Location: "System", LaunchPath: "/Applications/Cursor.app"},
		{Name: "Cursor", Location: "User", LaunchPath: "/Users/x/Applications/Cursor.app"},
		{Name: "Zed", Location: "System", LaunchPath: "/Applications/Zed.app"},
	}
}

func TestNewIDEPickerSelectsRemembered(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "/Applications/Zed.app", "/repo")
	if p.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (remembered Zed)", p.cursor)
	}
}

func TestNewIDEPickerFallsBackToFirstWhenMissing(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "/Applications/Gone.app", "/repo")
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (fallback to first)", p.cursor)
	}
}

func TestNewIDEPickerFallsBackToFirstWhenNoneRemembered(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "", "/repo")
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0", p.cursor)
	}
}

// TestIDEPickerClickOpensImmediately guards the click path: clicking an install
// must select *and* open it, not merely move the cursor and wait for enter.
func TestIDEPickerClickOpensImmediately(t *testing.T) {
	const termW, termH = 120, 60
	const want = 2 // click the third install, not the pre-selected first

	p := NewIDEPicker(sampleInstalls(), "", "/repo")
	p.SetSize(termW, termH)
	p.Show()

	// Populate hitRegions and derive the same coordinates the compositor uses.
	b := p.build()
	dialogW, dialogH := b.Size()
	dialogX, dialogY := centerOrigin(termW, termH, dialogW, dialogH)
	contentX, contentY := b.ContentOffset()

	var reg HitRegion
	found := false
	for _, h := range p.hitRegions {
		if h.index == want {
			reg, found = h.region, true
			break
		}
	}
	if !found {
		t.Fatalf("no hit region for install %d — every row must be clickable", want)
	}

	mx := dialogX + contentX + reg.X + reg.Width/2
	my := dialogY + contentY + reg.Y + reg.Height/2
	_, cmd := p.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: mx, Y: my})

	if cmd == nil {
		t.Fatal("click returned no command — the IDE was not opened")
	}
	res, ok := cmd().(IDEPickerResult)
	if !ok {
		t.Fatalf("click produced %T, want IDEPickerResult", cmd())
	}
	if !res.Confirmed {
		t.Error("click produced an unconfirmed result — the IDE was not opened")
	}
	if res.Install.LaunchPath != sampleInstalls()[want].LaunchPath {
		t.Errorf("opened %q, want %q", res.Install.LaunchPath, sampleInstalls()[want].LaunchPath)
	}
	if res.Root != "/repo" {
		t.Errorf("root = %q, want /repo", res.Root)
	}
	if p.Visible() {
		t.Error("picker still visible after a click that opened an IDE")
	}
}
