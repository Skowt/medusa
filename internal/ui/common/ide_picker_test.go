package common

import (
	"strings"
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
	p := NewIDEPicker(sampleInstalls(), "/Applications/Zed.app", "/repo", false)
	if p.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (remembered Zed)", p.cursor)
	}
}

func TestNewIDEPickerFallsBackToFirstWhenMissing(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "/Applications/Gone.app", "/repo", false)
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (fallback to first)", p.cursor)
	}
}

func TestNewIDEPickerFallsBackToFirstWhenNoneRemembered(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "", "/repo", false)
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0", p.cursor)
	}
}

// TestIDEPickerClickOpensImmediately guards the click path: clicking an install
// must select *and* open it, not merely move the cursor and wait for enter.
func TestIDEPickerClickOpensImmediately(t *testing.T) {
	const termW, termH = 120, 60
	const want = 2 // click the third install, not the pre-selected first

	p := NewIDEPicker(sampleInstalls(), "", "/repo", false)
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

// clickRow drives a real mouse click at the centre of the picker row whose hit
// region carries index, using the same geometry the compositor does.
func clickRow(t *testing.T, p *IDEPicker, index, termW, termH int) tea.Cmd {
	t.Helper()
	b := p.build()
	dialogW, dialogH := b.Size()
	dialogX, dialogY := centerOrigin(termW, termH, dialogW, dialogH)
	contentX, contentY := b.ContentOffset()

	for _, h := range p.hitRegions {
		if h.index != index {
			continue
		}
		mx := dialogX + contentX + h.region.X + h.region.Width/2
		my := dialogY + contentY + h.region.Y + h.region.Height/2
		_, cmd := p.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: mx, Y: my})
		return cmd
	}
	t.Fatalf("no hit region for row %d", index)
	return nil
}

// TestIDEPickerCheckboxClickTogglesWithoutOpening guards the one row that is
// not an install: clicking it must tick the box and leave the picker up, never
// launch whatever IDE the cursor happened to be on.
func TestIDEPickerCheckboxClickTogglesWithoutOpening(t *testing.T) {
	const termW, termH = 120, 60
	p := NewIDEPicker(sampleInstalls(), "", "/repo", false)
	p.SetSize(termW, termH)
	p.Show()

	if cmd := clickRow(t, p, len(sampleInstalls()), termW, termH); cmd != nil {
		t.Fatalf("clicking the checkbox produced %T — it must not open an IDE", cmd())
	}
	if !p.dontAsk {
		t.Error("clicking the checkbox did not tick it")
	}
	if !p.Visible() {
		t.Error("picker closed on a checkbox click")
	}

	if cmd := clickRow(t, p, len(sampleInstalls()), termW, termH); cmd != nil {
		t.Fatal("second checkbox click produced a command")
	}
	if p.dontAsk {
		t.Error("clicking the checkbox twice did not untick it")
	}
}

// TestIDEPickerKeyboardTogglesAndCarriesTheChoice covers the keyboard path
// end to end: the checkbox is one row past the last install, enter toggles it
// there, and confirming an install afterwards carries the choice out.
func TestIDEPickerKeyboardTogglesAndCarriesTheChoice(t *testing.T) {
	installs := sampleInstalls()
	p := NewIDEPicker(installs, "", "/repo", false)
	p.SetSize(120, 60)
	p.Show()

	down := tea.KeyPressMsg{Code: 'j', Text: "j"}
	for range installs {
		p, _ = p.Update(down)
	}
	if p.cursor != p.checkboxIndex() {
		t.Fatalf("cursor = %d after %d downs, want the checkbox row %d", p.cursor, len(installs), p.checkboxIndex())
	}

	if _, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("enter on the checkbox produced %T — it must only toggle", cmd())
	}
	if !p.dontAsk {
		t.Fatal("enter on the checkbox did not tick it")
	}

	// One more down wraps back to the first install, which enter then opens.
	p, _ = p.Update(down)
	if p.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (wrapped past the checkbox)", p.cursor)
	}
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on an install produced no command")
	}
	res, ok := cmd().(IDEPickerResult)
	if !ok {
		t.Fatalf("got %T, want IDEPickerResult", cmd())
	}
	if !res.Confirmed || !res.DontAskAgain {
		t.Errorf("result = {Confirmed:%v DontAskAgain:%v}, want both true", res.Confirmed, res.DontAskAgain)
	}
}

// TestIDEPickerCancelCarriesNoPreference guards Esc: a cancelled picker must
// not report DontAskAgain, or backing out of the dialog would silently turn
// the prompt off for good.
func TestIDEPickerCancelCarriesNoPreference(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "", "/repo", false)
	p.SetSize(120, 60)
	p.Show()
	p.cursor = p.checkboxIndex()
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	res, ok := cmd().(IDEPickerResult)
	if !ok {
		t.Fatalf("esc produced %T, want IDEPickerResult", cmd())
	}
	if res.Confirmed || res.DontAskAgain {
		t.Errorf("cancel produced %+v, want an empty result", res)
	}
}

// TestIDEPickerShowsThePreferenceItWasGiven covers the case that reopens the
// picker with the setting already on — a remembered IDE that has since been
// uninstalled. The box must show the stored value, not an unticked default.
func TestIDEPickerShowsThePreferenceItWasGiven(t *testing.T) {
	p := NewIDEPicker(sampleInstalls(), "/Applications/Gone.app", "/repo", true)
	if !p.dontAsk {
		t.Fatal("picker dropped the stored preference")
	}
	p.SetSize(120, 60)
	p.Show()
	if !strings.Contains(flatten(p.View()), "["+Icons.Clean+"] Don't ask again") {
		t.Error("the checkbox rendered unticked despite the stored preference")
	}
}

// TestIDEPickerRemembersTheIDEActuallyOpened is the "I changed my mind" case:
// the checkbox commits to whichever install the user opens, not to the one the
// picker pre-selected, or ticking it while switching IDEs would pin the old one.
func TestIDEPickerRemembersTheIDEActuallyOpened(t *testing.T) {
	const termW, termH = 120, 60
	const want = 2 // Zed, while the remembered choice pre-selects Cursor

	p := NewIDEPicker(sampleInstalls(), "/Applications/Cursor.app", "/repo", true)
	p.SetSize(termW, termH)
	p.Show()
	if p.cursor != 0 {
		t.Fatalf("cursor = %d, want the remembered Cursor at 0", p.cursor)
	}

	cmd := clickRow(t, p, want, termW, termH)
	if cmd == nil {
		t.Fatal("clicking a different install produced no command")
	}
	res, ok := cmd().(IDEPickerResult)
	if !ok {
		t.Fatalf("got %T, want IDEPickerResult", cmd())
	}
	if res.Install.LaunchPath != sampleInstalls()[want].LaunchPath {
		t.Errorf("opened %q, want the newly clicked %q", res.Install.LaunchPath, sampleInstalls()[want].LaunchPath)
	}
	if !res.DontAskAgain {
		t.Error("the ticked checkbox did not survive picking a different IDE")
	}
}
