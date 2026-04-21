package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

func TestHandleGroupDialogResult_SetWorkspaceGroup_UngroupedOption(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := &data.Workspace{Name: "target"}
	cmd, handled := a.handleGroupDialogResult(DialogSetWorkspaceGroup, true, common.UngroupedOption, ws, "")
	if !handled {
		t.Fatal("expected handled=true")
	}
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	setMsg, ok := msg.(messages.SetWorkspaceGroup)
	if !ok {
		t.Fatalf("unexpected message type: %T", msg)
	}
	if setMsg.Label != "" {
		t.Errorf("expected empty label, got %q", setMsg.Label)
	}
	if setMsg.Workspace != ws {
		t.Errorf("unexpected workspace")
	}
}

func TestHandleGroupDialogResult_SetWorkspaceGroup_ExistingGroup(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := &data.Workspace{Name: "target"}
	cmd, handled := a.handleGroupDialogResult(DialogSetWorkspaceGroup, true, "shipping-q2", ws, "")
	if !handled {
		t.Fatal("expected handled=true")
	}
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	setMsg, ok := msg.(messages.SetWorkspaceGroup)
	if !ok {
		t.Fatalf("unexpected message type: %T", msg)
	}
	if setMsg.Label != "shipping-q2" {
		t.Errorf("expected 'shipping-q2', got %q", setMsg.Label)
	}
}

func TestHandleGroupDialogResult_SetWorkspaceGroup_NewGroupOption_SwapsDialog(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := &data.Workspace{Name: "target"}
	cmd, handled := a.handleGroupDialogResult(DialogSetWorkspaceGroup, true, common.NewGroupOption, ws, "")
	if !handled {
		t.Fatal("expected handled=true")
	}
	if cmd != nil {
		t.Errorf("expected no dispatch command when swapping to input dialog, got %v", cmd)
	}
	// a.dialog should now be a text input dialog.
	if a.dialog == nil {
		t.Fatal("expected a.dialog to be set to input dialog")
	}
	// a.dialogWorkspace should still hold the workspace for the second-round submission.
	if a.dialogWorkspace != ws {
		t.Errorf("dialogWorkspace not preserved after swap")
	}
}

func TestHandleGroupDialogResult_NotConfirmed_CancelsWithoutDispatch(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := &data.Workspace{Name: "target"}
	cmd, handled := a.handleGroupDialogResult(DialogSetWorkspaceGroup, false, "anything", ws, "")
	if !handled {
		t.Fatal("expected handled=true (we own this dialog ID even on cancel)")
	}
	if cmd != nil {
		t.Errorf("expected nil cmd on cancel, got %v", cmd)
	}
}
