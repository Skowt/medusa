package gitreview

import (
	"sync"
	"testing"
)

// prefRecorder captures what the host is told to remember.
type prefRecorder struct {
	mu   sync.Mutex
	seen []Preferences
}

func (r *prefRecorder) note(p Preferences) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, p)
}

func (r *prefRecorder) last() (Preferences, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.seen) == 0 {
		return Preferences{}, false
	}
	return r.seen[len(r.seen)-1], true
}

// TestTickingAutoSendIsReportedToTheHost: the mode is a habit, not a per-review
// decision, so the host has to hear about it to open the next review the same way.
func TestTickingAutoSendIsReportedToTheHost(t *testing.T) {
	ts, service, session, _ := harness(t)
	rec := &prefRecorder{}
	service.OnPreferences(rec.note)

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/autosend", autoSendRequest{AutoSend: true})
	_ = res.Body.Close()

	got, ok := rec.last()
	if !ok {
		t.Fatal("the host was not told the mode changed")
	}
	if !got.AutoSend {
		t.Errorf("reported AutoSend = false after ticking it: %+v", got)
	}
	// The scope rides along, so a caller that changes one cannot overwrite the
	// other with a stale value.
	if got.Scope != session.Scope() {
		t.Errorf("reported scope %q, want %q", got.Scope, session.Scope())
	}
}

// TestSwitchingScopeIsReportedToTheHost covers the other sticky setting.
func TestSwitchingScopeIsReportedToTheHost(t *testing.T) {
	ts, service, session, _ := harness(t)
	rec := &prefRecorder{}
	service.OnPreferences(rec.note)

	// The harness opens on branch, so ask for the other one.
	res, err := ts.Client().Get(ts.URL + "/s/" + session.ID() + "/api/snapshot?scope=working")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	got, ok := rec.last()
	if !ok {
		t.Fatal("the host was not told the scope changed")
	}
	if got.Scope != ScopeWorking {
		t.Errorf("reported scope %q, want %q", got.Scope, ScopeWorking)
	}
}

// TestAskingForTheScopeAlreadyShownReportsNothing: the page asks for its scope on
// every load, so reporting unconditionally would write config on every refresh.
func TestAskingForTheScopeAlreadyShownReportsNothing(t *testing.T) {
	ts, service, session, _ := harness(t)
	rec := &prefRecorder{}
	service.OnPreferences(rec.note)

	res, err := ts.Client().Get(ts.URL + "/s/" + session.ID() + "/api/snapshot?scope=branch")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if got, ok := rec.last(); ok {
		t.Errorf("a no-op scope request was reported as a change: %+v", got)
	}
}

// TestOpenSeedsTheSessionFromThePreferences is the other half: remembering a
// setting is pointless if the next review does not start with it.
func TestOpenSeedsTheSessionFromThePreferences(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	service := NewService(t.TempDir(), nil)
	t.Cleanup(func() { _ = service.Close() })

	if _, err := service.Open(OpenRequest{
		Root: root, Name: "demo", AgentSession: "agent",
		Scope: ScopeBranch, AutoSend: true, Split: true,
	}); err != nil {
		t.Fatalf("open: %v", err)
	}

	sessions := service.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions", len(sessions))
	}
	if !sessions[0].AutoSend() {
		t.Error("the session did not open with auto-send on")
	}
	if sessions[0].Scope() != ScopeBranch {
		t.Errorf("the session opened on scope %q, want branch", sessions[0].Scope())
	}
	if !sessions[0].Split() {
		t.Error("the session did not open in the split view")
	}
}

// TestAServiceWithNoPreferenceSinkDoesNotPanic: a Service built without one --
// which is every test, and any host that does not remember settings -- still works.
func TestAServiceWithNoPreferenceSinkDoesNotPanic(t *testing.T) {
	ts, _, session, _ := harness(t)
	res := postJSON(t, ts, "/s/"+session.ID()+"/api/autosend", autoSendRequest{AutoSend: true})
	_ = res.Body.Close()
	if !session.AutoSend() {
		t.Error("the mode did not change without a preference sink")
	}
}

// TestSwitchingViewIsReportedToTheHost covers the third sticky setting. The
// layout changes nothing the server does, so it is carried only because the page
// has nowhere durable of its own: each review is served from a fresh ephemeral
// port, and anything the browser stored would be lost on the next restart.
func TestSwitchingViewIsReportedToTheHost(t *testing.T) {
	ts, service, session, _ := harness(t)
	rec := &prefRecorder{}
	service.OnPreferences(rec.note)

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/view", viewRequest{Split: boolPtr(true)})
	_ = res.Body.Close()

	got, ok := rec.last()
	if !ok {
		t.Fatal("the host was not told the view changed")
	}
	if !got.Split {
		t.Errorf("reported Split = false after switching to it: %+v", got)
	}
	if !session.Split() {
		t.Error("the session did not record the split view")
	}
}

// TestChangingOneSettingDoesNotClearAnother is why notePrefs reads every value
// off the session rather than taking the changed one as an argument.
func TestChangingOneSettingDoesNotClearAnother(t *testing.T) {
	ts, service, session, _ := harness(t)
	rec := &prefRecorder{}
	service.OnPreferences(rec.note)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/view", viewRequest{Split: boolPtr(true)})
	_ = res.Body.Close()
	res = postJSON(t, ts, base+"/autosend", autoSendRequest{AutoSend: true})
	_ = res.Body.Close()

	got, _ := rec.last()
	if !got.Split {
		t.Error("turning auto-send on reported the view back to unified")
	}
	if !got.AutoSend {
		t.Error("auto-send was not reported")
	}
	if got.Scope != session.Scope() {
		t.Errorf("scope came back as %q, want %q", got.Scope, session.Scope())
	}
}

func boolPtr(v bool) *bool { return &v }

func strPtr(v string) *string { return &v }

// TestSettingOneAppearanceFieldLeavesTheOtherAlone is why viewRequest's fields
// are pointers. Plain values would make every request an assertion about
// everything, and a page with one of them stale would overwrite the other.
func TestSettingOneAppearanceFieldLeavesTheOtherAlone(t *testing.T) {
	ts, _, session, _ := harness(t)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/view", viewRequest{Split: boolPtr(true)})
	_ = res.Body.Close()
	res = postJSON(t, ts, base+"/view", viewRequest{Theme: strPtr("light")})
	_ = res.Body.Close()

	if !session.Split() {
		t.Error("setting the theme turned the split view off")
	}
	if session.Theme() != ThemeLight {
		t.Errorf("theme = %q, want light", session.Theme())
	}

	// And an unrecognised theme is normalised rather than stored.
	res = postJSON(t, ts, base+"/view", viewRequest{Theme: strPtr("solarized")})
	_ = res.Body.Close()
	if session.Theme() != ThemeSystem {
		t.Errorf("an unknown theme was stored as %q, want the system default", session.Theme())
	}
	if !session.Split() {
		t.Error("a rejected theme took the layout with it")
	}
}
