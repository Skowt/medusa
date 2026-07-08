package vterm

import (
	"testing"
	"time"
)

// newStalledSyncVTerm begins a synchronized-output region and stubs syncNow
// so the region already looks older than SyncStallTimeout. Callers must
// invoke the returned restore func (defer) to put syncNow back.
func newStalledSyncVTerm(t *testing.T) (vt *VTerm, restore func()) {
	t.Helper()
	vt = New(5, 2)
	vt.Scrollback = [][]Cell{MakeBlankLine(5)}
	vt.Screen = [][]Cell{MakeBlankLine(5), MakeBlankLine(5)}

	start := time.Now()
	vt.setSynchronizedOutput(true)

	orig := syncNow
	syncNow = func() time.Time {
		return start.Add(SyncStallTimeout + time.Second)
	}

	return vt, func() { syncNow = orig }
}

func TestSyncStallReleasesOnRenderBuffers(t *testing.T) {
	vt, restore := newStalledSyncVTerm(t)
	defer restore()

	// Mutate the live screen after the sync region began so the frozen
	// snapshot and the live buffers are distinguishable.
	vt.Screen = [][]Cell{MakeBlankLine(5), MakeBlankLine(5), MakeBlankLine(5)}

	screen, scrollbackLen := vt.RenderBuffers()
	if len(screen) != len(vt.Screen) {
		t.Fatalf("expected RenderBuffers to return the live screen (%d lines) after the stall timeout, got %d lines (still frozen)", len(vt.Screen), len(screen))
	}
	if scrollbackLen != len(vt.Scrollback) {
		t.Fatalf("expected RenderBuffers to return the live scrollback length %d after the stall timeout, got %d", len(vt.Scrollback), scrollbackLen)
	}
	if vt.SyncActive() {
		t.Fatalf("expected syncActive to be false after RenderBuffers releases a stalled sync region")
	}
}

func TestSyncStallReleasesOnWrite(t *testing.T) {
	vt, restore := newStalledSyncVTerm(t)
	defer restore()

	vt.Write([]byte("x"))

	if vt.SyncActive() {
		t.Fatalf("expected syncActive to be false after Write releases a stalled sync region")
	}
}

func TestSyncStallReleasesOnVersion(t *testing.T) {
	vt, restore := newStalledSyncVTerm(t)
	defer restore()

	_ = vt.Version()

	if vt.SyncActive() {
		t.Fatalf("expected syncActive to be false after Version releases a stalled sync region")
	}
}

func TestSyncStallNotReleasedBeforeTimeout(t *testing.T) {
	vt := New(5, 2)
	vt.Scrollback = [][]Cell{MakeBlankLine(5)}
	vt.Screen = [][]Cell{MakeBlankLine(5), MakeBlankLine(5)}

	vt.setSynchronizedOutput(true)
	if !vt.SyncActive() {
		t.Fatalf("expected syncActive to be true immediately after setSynchronizedOutput(true)")
	}

	vt.RenderBuffers()
	if !vt.SyncActive() {
		t.Fatalf("expected syncActive to remain true before SyncStallTimeout elapses")
	}
}
