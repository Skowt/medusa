package app

import (
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/update"
)

func updateCheck() messages.UpdateCheckComplete {
	return messages.UpdateCheckComplete{
		CurrentVersion:  "0.0.4",
		LatestVersion:   "0.0.5",
		UpdateAvailable: true,
	}
}

// A user whose binary sits in a root-owned directory must not be told to open
// Settings and click a button that cannot work.
func TestUpdateToastWarnsWhenSelfUpdateBlocked(t *testing.T) {
	a := &App{
		toast: common.NewToastModel(),
		selfUpdate: update.SelfUpdateStatus{
			BinaryPath: "/usr/local/bin/medusa",
			Writable:   false,
		},
	}

	if cmd := a.handleUpdateCheckComplete(updateCheck()); cmd == nil {
		t.Fatal("expected a toast command on first discovery of an update")
	}

	view := a.toast.View(200)
	if !strings.Contains(view, "/usr/local/bin") {
		t.Errorf("toast should name the unwritable directory, got: %s", view)
	}
	if !strings.Contains(view, "not writable") {
		t.Errorf("toast should say the directory is not writable, got: %s", view)
	}
	if strings.Contains(view, "open Settings to install") {
		t.Errorf("toast must not point at the unusable Settings button, got: %s", view)
	}
}

func TestUpdateToastPointsAtSettingsWhenWritable(t *testing.T) {
	a := &App{
		toast: common.NewToastModel(),
		selfUpdate: update.SelfUpdateStatus{
			BinaryPath: "/home/me/.local/bin/medusa",
			Writable:   true,
		},
	}

	if cmd := a.handleUpdateCheckComplete(updateCheck()); cmd == nil {
		t.Fatal("expected a toast command on first discovery of an update")
	}

	view := a.toast.View(200)
	if !strings.Contains(view, "open Settings to install") {
		t.Errorf("writable install should be told to use Settings, got: %s", view)
	}
	if strings.Contains(view, "not writable") {
		t.Errorf("writable install must not be warned, got: %s", view)
	}
}

// A `go install` build is unwritable-adjacent but has its own remedy, surfaced
// when the user actually tries to upgrade. It must not trip the reinstall path.
func TestUpdateToastDoesNotWarnForGoInstall(t *testing.T) {
	a := &App{
		toast: common.NewToastModel(),
		selfUpdate: update.SelfUpdateStatus{
			BinaryPath: "/home/me/go/bin/medusa",
			GoInstall:  true,
			Writable:   false,
		},
	}

	if cmd := a.handleUpdateCheckComplete(updateCheck()); cmd == nil {
		t.Fatal("expected a toast command on first discovery of an update")
	}

	if view := a.toast.View(200); strings.Contains(view, "not writable") {
		t.Errorf("go install builds must not get the reinstall warning, got: %s", view)
	}
}

// The toast fires once; a second check must not re-toast.
func TestUpdateToastShownOnlyOnce(t *testing.T) {
	a := &App{
		toast:      common.NewToastModel(),
		selfUpdate: update.SelfUpdateStatus{BinaryPath: "/usr/local/bin/medusa"},
	}

	if cmd := a.handleUpdateCheckComplete(updateCheck()); cmd == nil {
		t.Fatal("expected a toast on first discovery")
	}
	if cmd := a.handleUpdateCheckComplete(updateCheck()); cmd != nil {
		t.Error("expected no second toast for the same update")
	}
}
