package common

import (
	"testing"

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
