package skillstats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer builds a server over a store seeded with two profiles' worth of
// invocations.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, profilesRoot, _ := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "s1",
		skillLine("u1", nowStamp(), "s1", "/tmp", "plug:alpha", false)+
			skillLine("u2", nowStamp(), "s1", "/tmp", "plug:beta", true))
	writeTranscript(t, profilesRoot, "Default", "proj", "s2",
		skillLine("u3", nowStamp(), "s2", "/tmp", "humanizer", false))
	store.Scan()

	srv := httptest.NewServer(NewServer(store).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// nowStamp returns a timestamp inside every granularity's default window so
// seeded events appear in the hourly view too.
func nowStamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// TestHandlePageServesDashboard verifies the embedded page is served and is
// self-contained: a CDN reference would leave the dashboard blank offline.
func TestHandlePageServesDashboard(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	body := readAll(t, resp)
	for _, want := range []string{"Skill Spy", "api/stats", "api/meta"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, bad := range []string{"https://cdn", "src=\"http", "@import url(http"} {
		if strings.Contains(body, bad) {
			t.Errorf("page loads an external asset (%q); it must work offline", bad)
		}
	}
}

// TestDashboardExplainsItsDataSource verifies the page states where its numbers
// come from. Without it a reader cannot tell whether a low count means a skill
// went unused or simply was not captured, and the /slash gap is invisible.
func TestDashboardExplainsItsDataSource(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readAll(t, resp)

	for _, want := range []string{"transcripts", "/slash", "not counted"} {
		if !strings.Contains(body, want) {
			t.Errorf("info box does not mention %q", want)
		}
	}
}

// TestHandleStatsPerGranularity verifies each view returns its default window
// and that events are grouped by plugin with skills still distinguishable.
func TestHandleStatsPerGranularity(t *testing.T) {
	srv := newTestServer(t)
	for _, gran := range []Granularity{GranHour, GranDay, GranWeek} {
		var stats Stats
		getJSON(t, srv.URL+"/api/stats?gran="+string(gran), &stats)

		if stats.Gran != gran {
			t.Errorf("%s: gran = %q", gran, stats.Gran)
		}
		if len(stats.Buckets) != DefaultBuckets(gran) {
			t.Errorf("%s: %d buckets, want default %d", gran, len(stats.Buckets), DefaultBuckets(gran))
		}
		if stats.Total != 3 {
			t.Errorf("%s: total = %d, want 3", gran, stats.Total)
		}
		if stats.Subagent != 1 {
			t.Errorf("%s: subagent = %d, want 1", gran, stats.Subagent)
		}

		byPlugin := map[string]PluginStat{}
		for _, p := range stats.Plugins {
			byPlugin[p.Plugin] = p
		}
		plug, ok := byPlugin["plug"]
		if !ok {
			t.Fatalf("%s: plugin group missing from %+v", gran, stats.Plugins)
		}
		if plug.Total != 2 || len(plug.Skills) != 2 {
			t.Errorf("%s: plug = %+v, want 2 invocations across 2 skills", gran, plug)
		}
		if _, ok := byPlugin[GroupPersonal]; !ok {
			if _, builtin := byPlugin[GroupBuiltin]; !builtin {
				t.Errorf("%s: bare-name skill not bucketed: %+v", gran, stats.Plugins)
			}
		}
	}
}

// TestHandleStatsProfileFilter verifies the profile dimension reaches the API.
func TestHandleStatsProfileFilter(t *testing.T) {
	srv := newTestServer(t)
	var stats Stats
	getJSON(t, srv.URL+"/api/stats?gran=day&profile=Work", &stats)
	if stats.Total != 2 {
		t.Errorf("Work total = %d, want 2", stats.Total)
	}
	for _, p := range stats.Plugins {
		if p.Plugin != "plug" {
			t.Errorf("Work view leaked plugin %q from another profile", p.Plugin)
		}
	}

	var unknown Stats
	getJSON(t, srv.URL+"/api/stats?gran=day&profile=Nope", &unknown)
	if unknown.Total != 0 || len(unknown.Buckets) != DefaultBuckets(GranDay) {
		t.Errorf("unknown profile = %d events in %d buckets, want 0 events with an intact series",
			unknown.Total, len(unknown.Buckets))
	}
}

// TestHandleStatsBucketOverride verifies an explicit window is honoured and
// clamped.
func TestHandleStatsBucketOverride(t *testing.T) {
	srv := newTestServer(t)
	var stats Stats
	getJSON(t, srv.URL+"/api/stats?gran=day&buckets=7", &stats)
	if len(stats.Buckets) != 7 {
		t.Errorf("buckets = %d, want 7", len(stats.Buckets))
	}

	var clamped Stats
	getJSON(t, srv.URL+"/api/stats?gran=day&buckets=99999", &clamped)
	if len(clamped.Buckets) != maxBuckets {
		t.Errorf("buckets = %d, want clamp to %d", len(clamped.Buckets), maxBuckets)
	}

	var garbage Stats
	getJSON(t, srv.URL+"/api/stats?gran=day&buckets=abc", &garbage)
	if len(garbage.Buckets) != DefaultBuckets(GranDay) {
		t.Errorf("buckets = %d, want default on unparseable input", len(garbage.Buckets))
	}
}

// TestHandleMetaAdvertisesProfilesAndDefaults verifies the page can build its
// controls from one call.
func TestHandleMetaAdvertisesProfilesAndDefaults(t *testing.T) {
	srv := newTestServer(t)
	var meta metaResponse
	getJSON(t, srv.URL+"/api/meta", &meta)

	if len(meta.Profiles) != 2 || meta.Profiles[0] != "Default" || meta.Profiles[1] != "Work" {
		t.Errorf("profiles = %v, want [Default Work]", meta.Profiles)
	}
	if meta.Total != 3 {
		t.Errorf("total = %d, want 3", meta.Total)
	}
	got := map[Granularity]int{}
	for _, d := range meta.Defaults {
		got[d.Gran] = d.Buckets
	}
	for gran, want := range defaultBuckets {
		if got[gran] != want {
			t.Errorf("default for %s = %d, want %d", gran, got[gran], want)
		}
	}
}

// TestHandleScanReturnsSummary verifies the forced rescan endpoint reports the
// counters the page displays.
func TestHandleScanReturnsSummary(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Post(srv.URL+"/api/scan", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var result ScanResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.NewEvents != 0 {
		t.Errorf("rescan added %d events, want 0", result.NewEvents)
	}
	if result.FilesConsidered != 2 {
		t.Errorf("files considered = %d, want 2", result.FilesConsidered)
	}
}

// TestUnknownPathIsNotFound verifies stray paths do not fall through to the page.
func TestUnknownPathIsNotFound(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
