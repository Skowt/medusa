package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/ide"
)

func ideTestInstalls() []ide.Install {
	return []ide.Install{
		{Name: "Cursor", Location: "System", LaunchPath: "/Applications/Cursor.app"},
		{Name: "Zed", Location: "System", LaunchPath: "/Applications/Zed.app"},
	}
}

// TestRememberedIDE covers the three states the IDE button can be pressed in.
// The uninstalled case is the one worth guarding: skipping the picker on a
// path that no longer resolves would leave the button silently doing nothing.
func TestRememberedIDE(t *testing.T) {
	cases := []struct {
		name       string
		remembered string
		skipPicker bool
		wantOpen   string // "" means the picker must be shown
	}{
		{"prompt is on", "/Applications/Zed.app", false, ""},
		{"prompt is off and the IDE is installed", "/Applications/Zed.app", true, "/Applications/Zed.app"},
		{"prompt is off but the IDE is gone", "/Applications/Gone.app", true, ""},
		{"prompt is off with nothing remembered", "", true, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			install, ok := rememberedIDE(ideTestInstalls(), c.remembered, c.skipPicker)
			if ok != (c.wantOpen != "") {
				t.Fatalf("open = %v, want %v", ok, c.wantOpen != "")
			}
			if ok && install.LaunchPath != c.wantOpen {
				t.Errorf("opened %q, want %q", install.LaunchPath, c.wantOpen)
			}
		})
	}
}
