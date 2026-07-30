package skillstats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore builds a store over a temp profiles tree.
func newTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	base := t.TempDir()
	profilesRoot := filepath.Join(base, "profiles")
	storeDir := filepath.Join(base, "skill-usage")
	store, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	return store, profilesRoot, storeDir
}

// TestScanIsIdempotent verifies a rescan of unchanged transcripts adds nothing.
// Double counting here would silently inflate every number on the dashboard.
func TestScanIsIdempotent(t *testing.T) {
	store, profilesRoot, _ := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false)+
			skillLine("u2", "2026-07-30T10:05:00.000Z", "s", "/tmp", "a:two", false))

	first := store.Scan()
	if first.NewEvents != 2 {
		t.Fatalf("first scan found %d events, want 2", first.NewEvents)
	}
	second := store.Scan()
	if second.NewEvents != 0 {
		t.Errorf("rescan added %d events, want 0", second.NewEvents)
	}
	if second.FilesRead != 0 {
		t.Errorf("rescan read %d files, want 0 (unchanged by size+mtime)", second.FilesRead)
	}
	if got := len(store.Events()); got != 2 {
		t.Errorf("stored %d events, want 2", got)
	}
}

// TestScanDedupesAcrossForcedRescan verifies dedup survives a state file loss,
// which forces every transcript to be reparsed from byte zero.
func TestScanDedupesAcrossForcedRescan(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false))
	store.Scan()

	if err := os.Remove(filepath.Join(storeDir, stateFileName)); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	result := reopened.Scan()
	if result.FilesRead != 1 {
		t.Errorf("files read = %d, want 1 (state lost, full reparse)", result.FilesRead)
	}
	if result.NewEvents != 0 {
		t.Errorf("full reparse added %d events, want 0", result.NewEvents)
	}
	if got := len(reopened.Events()); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}
}

// TestStoreOutlivesDeletedTranscript is the reason the durable log exists:
// Claude Code prunes transcripts after 30 days by default, but the dashboard
// reports over months, so a scanned event must survive its source file.
func TestStoreOutlivesDeletedTranscript(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	path := writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false))
	if store.Scan().NewEvents != 1 {
		t.Fatal("setup scan found no events")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	reopened.Scan()

	events := reopened.Events()
	if len(events) != 1 || events[0].Skill != "one" {
		t.Fatalf("events after transcript deletion = %+v, want the recorded invocation", events)
	}
}

// TestStoreReloadsPersistedEvents verifies a reopened store recovers its history
// and its derived views from the log alone.
func TestStoreReloadsPersistedEvents(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "s1",
		skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false))
	writeTranscript(t, profilesRoot, "Default", "proj", "s2",
		skillLine("u2", "2026-07-30T10:00:00.000Z", "s", "/tmp", "humanizer", false))
	store.Scan()

	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reopened.Events()); got != 2 {
		t.Fatalf("reloaded %d events, want 2", got)
	}
	profiles := reopened.Profiles()
	if len(profiles) != 2 || profiles[0] != "Default" || profiles[1] != "Work" {
		t.Errorf("profiles = %v, want [Default Work] sorted", profiles)
	}
}

// TestStoreSkipsTruncatedLogLine verifies a log truncated mid-write loads the
// intact records instead of failing outright.
func TestStoreSkipsTruncatedLogLine(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false))
	store.Scan()

	logPath := filepath.Join(storeDir, eventsFileName)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, append(data, []byte(`{"uuid":"half`)...), 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatalf("truncated log failed the load: %v", err)
	}
	if got := len(reopened.Events()); got != 1 {
		t.Errorf("events = %d, want 1 intact record", got)
	}
}

// TestScanIncludesSystemProfile verifies invocations from Claude Code sessions
// run outside Medusa (against ~/.claude, so no CLAUDE_CONFIG_DIR) are counted
// and land under their own profile entry alongside the Medusa profiles.
func TestScanIncludesSystemProfile(t *testing.T) {
	base := t.TempDir()
	profilesRoot := filepath.Join(base, "profiles")
	claudeProjects := filepath.Join(base, "claude", "projects")

	writeTranscript(t, profilesRoot, "Work", "proj", "s1",
		skillLine("u1", stamp(1), "s1", "/tmp", "plug:alpha", false))

	// ~/.claude/projects has no profile level: transcripts sit directly under
	// their project dir.
	dir := filepath.Join(claudeProjects, "-Users-me-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := skillLine("u2", stamp(1), "s2", "/Users/me/code", "plug:beta", false) +
		skillLine("u3", stamp(2), "s2", "/Users/me/code", "dataviz", false)
	if err := os.WriteFile(filepath.Join(dir, "s2.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(filepath.Join(base, "skill-usage"), profilesRoot, claudeProjects)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Scan().NewEvents; got != 3 {
		t.Fatalf("scanned %d events, want 3", got)
	}

	profiles := store.Profiles()
	if len(profiles) != 2 {
		t.Fatalf("profiles = %v, want the Medusa profile plus %q", profiles, ClaudeProfileLabel)
	}
	var found bool
	for _, p := range profiles {
		if p == ClaudeProfileLabel {
			found = true
		}
	}
	if !found {
		t.Errorf("profiles = %v, missing %q", profiles, ClaudeProfileLabel)
	}

	// The entry must be filterable like any other profile.
	stats := Compute(store.Events(), Query{Gran: GranDay, Profile: ClaudeProfileLabel})
	if stats.Total != 2 {
		t.Errorf("%s total = %d, want 2", ClaudeProfileLabel, stats.Total)
	}
	for _, p := range stats.Plugins {
		for _, sk := range p.Skills {
			if sk.Skill == "alpha" {
				t.Errorf("%s view leaked a Medusa-profile invocation", ClaudeProfileLabel)
			}
		}
	}
}

// stamp renders a transcript timestamp offset from now by whole days.
func stamp(daysAgo int) string {
	return time.Now().AddDate(0, 0, -daysAgo).UTC().Format("2006-01-02T15:04:05.000Z")
}

// TestRetentionCutoffIsCalendarMonths verifies the window is six calendar months
// back, not a fixed day count that would drift across month lengths.
func TestRetentionCutoffIsCalendarMonths(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.January, 30, 12, 0, 0, 0, time.UTC)
	if got := RetentionCutoff(now); !got.Equal(want) {
		t.Errorf("cutoff = %s, want %s", got, want)
	}
}

// TestScanRejectsExpiredEvents verifies invocations older than the retention
// window never enter the store, even when their transcript still holds them.
func TestScanRejectsExpiredEvents(t *testing.T) {
	store, profilesRoot, _ := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("old", stamp(210), "s", "/tmp", "a:ancient", false)+
			skillLine("new", stamp(3), "s", "/tmp", "a:recent", false))

	result := store.Scan()
	if result.NewEvents != 1 {
		t.Fatalf("stored %d events, want 1 (the 210-day-old one is out of retention)", result.NewEvents)
	}
	events := store.Events()
	if len(events) != 1 || events[0].Skill != "recent" {
		t.Fatalf("events = %+v, want only the recent invocation", events)
	}
}

// TestPruneDropsExpiredOnLoad verifies aged-out records leave both memory and the
// file, so the log does not grow forever with lines filtered on every load.
func TestPruneDropsExpiredOnLoad(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("keep", stamp(10), "s", "/tmp", "a:recent", false))
	store.Scan()

	// Prepend a record that has since aged out, as a real log would contain.
	logPath := filepath.Join(storeDir, eventsFileName)
	existing, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	expired := Event{UUID: "expired", TS: time.Now().AddDate(0, -7, 0), Profile: "Work",
		Plugin: "a", Skill: "ancient"}
	line, err := json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, append(append(line, '\n'), existing...), 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	events := reopened.Events()
	if len(events) != 1 || events[0].Skill != "recent" {
		t.Fatalf("loaded %+v, want only the in-retention event", events)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "expired") {
		t.Error("expired record still in the log; load did not compact the file")
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 1 {
		t.Errorf("log has %d lines, want 1", lines)
	}
}

// TestPrunedEventsStayGoneAfterFullRescan is the pairing that makes pruning
// durable: the transcript still holds the aged-out invocation, so without the
// commit-path cutoff a state-file loss would resurrect it.
func TestPrunedEventsStayGoneAfterFullRescan(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("old", stamp(200), "s", "/tmp", "a:ancient", false)+
			skillLine("new", stamp(1), "s", "/tmp", "a:recent", false))
	store.Scan()

	if err := os.Remove(filepath.Join(storeDir, stateFileName)); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	result := reopened.Scan()
	if result.FilesRead != 1 {
		t.Fatalf("files read = %d, want a full reparse", result.FilesRead)
	}
	if result.NewEvents != 0 {
		t.Errorf("full rescan re-added %d expired events", result.NewEvents)
	}
	if got := len(reopened.Events()); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}
}

// TestPruneReportsCount verifies a scan reports what it dropped, so the number
// is visible rather than silent.
func TestPruneReportsCount(t *testing.T) {
	store, profilesRoot, storeDir := newTestStore(t)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("keep", stamp(5), "s", "/tmp", "a:recent", false))
	store.Scan()

	// Age out the stored record by rewriting the log behind the store's back,
	// then reopen so it is loaded and pruned on the next scan.
	logPath := filepath.Join(storeDir, eventsFileName)
	stale := Event{UUID: "stale", TS: time.Now().AddDate(0, -6, -1), Profile: "Work",
		Plugin: "a", Skill: "ancient"}
	line, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, append(append(line, '\n'), existing...), 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(storeDir, profilesRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	// Load already pruned it, so the surviving set is what matters here.
	if got := len(reopened.Events()); got != 1 {
		t.Errorf("events = %d, want 1 after load-time prune", got)
	}
}

// TestScanRereadsReplacedTranscript verifies a transcript that shrank is reread
// whole: its stored offset points into different content and would otherwise
// skip real invocations.
func TestScanRereadsReplacedTranscript(t *testing.T) {
	store, profilesRoot, _ := newTestStore(t)
	long := skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false) +
		skillLine("u2", "2026-07-30T10:01:00.000Z", "s", "/tmp", "a:two", false) +
		skillLine("u3", "2026-07-30T10:02:00.000Z", "s", "/tmp", "a:three", false)
	writeTranscript(t, profilesRoot, "Work", "proj", "sess", long)
	if got := store.Scan().NewEvents; got != 3 {
		t.Fatalf("setup scan found %d events, want 3", got)
	}

	// Replace with a shorter file whose only invocation sits before the offset.
	writeTranscript(t, profilesRoot, "Work", "proj", "sess",
		skillLine("u9", "2026-07-30T12:00:00.000Z", "s", "/tmp", "a:new", false))
	if got := store.Scan().NewEvents; got != 1 {
		t.Errorf("replaced transcript yielded %d new events, want 1", got)
	}
}

// TestScanEmptyTreeIsClean verifies scanning with no transcripts present is a
// no-op rather than an error.
func TestScanEmptyTreeIsClean(t *testing.T) {
	store, _, _ := newTestStore(t)
	result := store.Scan()
	if result.NewEvents != 0 || len(result.Errors) != 0 {
		t.Errorf("empty scan = %+v, want no events and no errors", result)
	}
	if store.LastScan().IsZero() {
		t.Error("LastScan not recorded after a scan")
	}
}
