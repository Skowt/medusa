package vterm

import "time"

// SyncStallTimeout bounds how long a synchronized-output region (DEC mode
// 2026) may stay open. If a writer dies mid-frame without ever sending the
// "end sync" sequence, RenderBuffers would otherwise stay frozen on the
// stale snapshot forever.
const SyncStallTimeout = 2 * time.Second

// syncNow returns the current time for sync stall tracking; tests may stub it.
var syncNow = time.Now

// maybeReleaseStaleSync force-closes an open sync region that has been open
// longer than SyncStallTimeout, acting as a failsafe against a writer that
// began a sync region and died before ending it.
func (v *VTerm) maybeReleaseStaleSync() {
	if v.syncActive && syncNow().Sub(v.syncStartedAt) > SyncStallTimeout {
		v.setSynchronizedOutput(false)
	}
}

// SyncActive reports whether synchronized output is currently active.
func (v *VTerm) SyncActive() bool {
	return v.syncActive
}

func (v *VTerm) setSynchronizedOutput(active bool) {
	if active {
		if v.syncActive {
			return
		}
		v.syncActive = true
		v.syncStartedAt = syncNow()
		v.syncScreen = copyScreen(v.Screen)
		v.syncScrollbackLen = len(v.Scrollback)
		v.invalidateRenderCache()
		return
	}

	if !v.syncActive {
		return
	}
	v.syncActive = false
	v.syncScreen = nil
	v.syncScrollbackLen = 0
	if v.syncDeferTrim {
		v.syncDeferTrim = false
		v.trimScrollback()
	}
	v.invalidateRenderCache()
}

func copyScreen(src [][]Cell) [][]Cell {
	dst := make([][]Cell, len(src))
	for i := range src {
		dst[i] = CopyLine(src[i])
	}
	return dst
}
