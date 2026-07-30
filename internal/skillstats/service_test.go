package skillstats

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestServiceStartsLazilyAndReuses verifies the first URL call brings the
// dashboard up and later calls return the same address instead of starting a
// second server on a second port.
func TestServiceStartsLazilyAndReuses(t *testing.T) {
	base := t.TempDir()
	profilesRoot := filepath.Join(base, "profiles")
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", nowStamp(), "s", "/tmp", "plug:alpha", false))

	svc := NewService(filepath.Join(base, "skill-usage"), profilesRoot, "")
	t.Cleanup(func() { _ = svc.Close() })

	url, err := svc.URL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("url = %q, want a loopback address", url)
	}

	again, err := svc.URL()
	if err != nil {
		t.Fatal(err)
	}
	if again != url {
		t.Errorf("second call returned %q, want the running server at %q", again, url)
	}

	resp, err := http.Get(url + "/api/stats?gran=day")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("stats status = %d", resp.StatusCode)
	}
}

// TestServiceScansOnStart verifies the dashboard has data as soon as it is
// reachable, rather than only after the page's first poll.
func TestServiceScansOnStart(t *testing.T) {
	base := t.TempDir()
	profilesRoot := filepath.Join(base, "profiles")
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", nowStamp(), "s", "/tmp", "plug:alpha", false))

	svc := NewService(filepath.Join(base, "skill-usage"), profilesRoot, "")
	t.Cleanup(func() { _ = svc.Close() })
	url, err := svc.URL()
	if err != nil {
		t.Fatal(err)
	}

	var meta metaResponse
	getJSON(t, url+"/api/meta", &meta)
	if meta.Total != 1 {
		t.Errorf("total = %d, want the transcript scanned before serving", meta.Total)
	}
}

// TestServiceCloseUnusedIsNoop verifies a host can close a service it never
// started, which is what an unconditional shutdown path does.
func TestServiceCloseUnusedIsNoop(t *testing.T) {
	svc := NewService(t.TempDir(), filepath.Join(t.TempDir(), "profiles"), "")
	if err := svc.Close(); err != nil {
		t.Errorf("Close on unused service = %v, want nil", err)
	}
}

// TestServiceRestartsAfterClose verifies a closed service can be reopened, so a
// stopped dashboard is recoverable without restarting the host.
func TestServiceRestartsAfterClose(t *testing.T) {
	base := t.TempDir()
	profilesRoot := filepath.Join(base, "profiles")
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", nowStamp(), "s", "/tmp", "plug:alpha", false))

	svc := NewService(filepath.Join(base, "skill-usage"), profilesRoot, "")
	first, err := svc.URL()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := svc.URL()
	if err != nil {
		t.Fatalf("restart after Close: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if second == first {
		t.Errorf("restart reused address %q; the old listener was closed", first)
	}

	resp, err := http.Get(second + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("restarted service status = %d", resp.StatusCode)
	}
}
