package skillstats

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSkill creates a minimal skill directory so resolver lookups find it.
func writeSkill(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// skillLine renders a transcript entry the way Claude Code writes one: compact
// JSON with a tool_use block for the Skill tool. Tests use the real wire shape
// rather than marshalling a struct, so a change to the shape fails here.
func skillLine(uuid, ts, session, cwd, skill string, sidechain bool) string {
	return fmt.Sprintf(
		`{"type":"assistant","uuid":%q,"timestamp":%q,"sessionId":%q,"cwd":%q,"isSidechain":%t,`+
			`"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Skill","input":{"skill":%q}}]}}`,
		uuid, ts, session, cwd, sidechain, skill) + "\n"
}

// writeTranscript places a transcript under a profile's projects tree, matching
// the layout Medusa produces via CLAUDE_CONFIG_DIR.
func writeTranscript(t *testing.T, profilesRoot, profile, project, session, body string) string {
	t.Helper()
	dir := filepath.Join(profilesRoot, profile, "projects", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, session+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseLineExtractsInvocation verifies the fields the dashboard depends on
// survive the parse.
func TestParseLineExtractsInvocation(t *testing.T) {
	line := skillLine("u1", "2026-07-30T10:15:00.000Z", "sess-1", "/tmp/wt", "cargo-ai-utils:explore", true)
	events := parseLine([]byte(line), "Work", newResolver())
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.UUID != "u1" || e.Session != "sess-1" || e.Profile != "Work" {
		t.Errorf("identity fields wrong: %+v", e)
	}
	if e.Plugin != "cargo-ai-utils" || e.Skill != "explore" {
		t.Errorf("grouping wrong: plugin=%q skill=%q", e.Plugin, e.Skill)
	}
	if !e.Sidechain {
		t.Error("subagent invocation not marked as sidechain")
	}
	if want := time.Date(2026, time.July, 30, 10, 15, 0, 0, time.UTC); !e.TS.Equal(want) {
		t.Errorf("timestamp = %s, want %s", e.TS, want)
	}
}

// TestParseLineMultipleSkillsPerEntry verifies two skills invoked in one
// assistant message are counted separately. Keying only on the entry uuid would
// collapse them into one.
func TestParseLineMultipleSkillsPerEntry(t *testing.T) {
	line := `{"type":"assistant","uuid":"u9","timestamp":"2026-07-30T10:00:00.000Z","sessionId":"s",` +
		`"message":{"content":[` +
		`{"type":"tool_use","name":"Skill","input":{"skill":"a:one"}},` +
		`{"type":"text","text":"chatter"},` +
		`{"type":"tool_use","name":"Skill","input":{"skill":"a:two"}}]}}` + "\n"

	events := parseLine([]byte(line), "Work", newResolver())
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].UUID == events[1].UUID {
		t.Errorf("both events share dedup key %q", events[0].UUID)
	}
	if events[0].Skill != "one" || events[1].Skill != "two" {
		t.Errorf("skills = %q, %q", events[0].Skill, events[1].Skill)
	}
}

// TestParseLineIgnoresNonSkillEntries verifies the parser is not fooled by other
// tools, prose mentioning the word, or malformed lines.
func TestParseLineIgnoresNonSkillEntries(t *testing.T) {
	lines := []string{
		`{"type":"assistant","uuid":"u2","timestamp":"2026-07-30T10:00:00.000Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-07-30T10:00:00.000Z","message":{"content":"use the Skill tool please"}}`,
		`{"type":"assistant","uuid":"u4","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"a:b"}}]}}`,                            // no timestamp
		`{"type":"assistant","timestamp":"2026-07-30T10:00:00.000Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"a:b"}}]}}`, // no uuid
		`{"broken json`,
		``,
	}
	for _, line := range lines {
		if events := parseLine([]byte(line+"\n"), "Work", newResolver()); len(events) != 0 {
			t.Errorf("line %q produced %d events, want 0", line, len(events))
		}
	}
}

// TestScanFileResumesFromOffset verifies an appended-to transcript is read from
// where the last scan stopped instead of reparsed whole.
func TestScanFileResumesFromOffset(t *testing.T) {
	root := t.TempDir()
	first := skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false)
	path := writeTranscript(t, root, "Work", "proj", "sess", first)

	events, offset, err := scanFile(path, "Work", 0, newResolver())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || offset != int64(len(first)) {
		t.Fatalf("first pass: %d events, offset %d (want 1, %d)", len(events), offset, len(first))
	}

	second := skillLine("u2", "2026-07-30T11:00:00.000Z", "s", "/tmp", "a:two", false)
	if err := os.WriteFile(path, []byte(first+second), 0o644); err != nil {
		t.Fatal(err)
	}
	events, offset, err = scanFile(path, "Work", offset, newResolver())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].UUID != "u2" {
		t.Fatalf("resumed pass returned %+v, want only u2", events)
	}
	if offset != int64(len(first)+len(second)) {
		t.Errorf("offset = %d, want %d", offset, len(first)+len(second))
	}
}

// TestScanFileHoldsBackPartialLine verifies a transcript captured mid-write does
// not advance the offset past its incomplete tail, so the invocation is picked
// up once the line is finished rather than lost.
func TestScanFileHoldsBackPartialLine(t *testing.T) {
	root := t.TempDir()
	complete := skillLine("u1", "2026-07-30T10:00:00.000Z", "s", "/tmp", "a:one", false)
	partial := skillLine("u2", "2026-07-30T11:00:00.000Z", "s", "/tmp", "a:two", false)
	truncated := partial[:len(partial)/2]
	path := writeTranscript(t, root, "Work", "proj", "sess", complete+truncated)

	events, offset, err := scanFile(path, "Work", 0, newResolver())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].UUID != "u1" {
		t.Fatalf("got %+v, want only the complete line", events)
	}
	if offset != int64(len(complete)) {
		t.Fatalf("offset = %d, want %d (partial line held back)", offset, len(complete))
	}

	if err := os.WriteFile(path, []byte(complete+partial), 0o644); err != nil {
		t.Fatal(err)
	}
	events, _, err = scanFile(path, "Work", offset, newResolver())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].UUID != "u2" {
		t.Errorf("after completion got %+v, want u2", events)
	}
}

// TestDiscoverRootsMapsProfiles verifies each profile's transcripts are labelled
// with that profile and non-Medusa sessions get the ~/.claude label.
func TestDiscoverRootsMapsProfiles(t *testing.T) {
	base := t.TempDir()
	profilesRoot := filepath.Join(base, "profiles")
	writeTranscript(t, profilesRoot, "Work", "p", "s", "")
	writeTranscript(t, profilesRoot, "Default", "p", "s", "")
	// A profile directory with no projects yet must not become a root.
	if err := os.MkdirAll(filepath.Join(profilesRoot, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(base, "claude-projects")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}

	roots := discoverRoots(profilesRoot, claude)
	got := map[string]string{}
	for _, r := range roots {
		got[r.profile] = r.dir
	}
	if len(roots) != 3 {
		t.Fatalf("roots = %+v, want Work, Default and %s", roots, ClaudeProfileLabel)
	}
	if got["Work"] != filepath.Join(profilesRoot, "Work", "projects") {
		t.Errorf("Work root = %q", got["Work"])
	}
	if got[ClaudeProfileLabel] != claude {
		t.Errorf("%s root = %q, want %q", ClaudeProfileLabel, got[ClaudeProfileLabel], claude)
	}
	if _, ok := got["shared"]; ok {
		t.Error("profile without a projects dir became a root")
	}
}

// TestDiscoverRootsToleratesMissingDirs verifies a fresh install with neither
// source present scans nothing instead of failing.
func TestDiscoverRootsToleratesMissingDirs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if roots := discoverRoots(missing, missing); len(roots) != 0 {
		t.Errorf("roots = %+v, want none", roots)
	}
}
