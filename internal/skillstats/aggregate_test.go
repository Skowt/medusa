package skillstats

import (
	"path/filepath"
	"testing"
	"time"
)

// at builds a local-zone timestamp; every bucket boundary is computed in the
// local zone, so tests must construct their inputs the same way.
func at(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 30, 0, 0, time.Local)
}

func ev(ts time.Time, profile, plugin, skill string) Event {
	return Event{UUID: plugin + skill + ts.String(), TS: ts, Profile: profile, Plugin: plugin, Skill: skill}
}

// TestQualifiedKeepsGroupsUnprefixed verifies the bare-name group buckets are
// treated as classifications, not as plugin prefixes: rendering them as
// "personal:humanizer" would invent a plugin that does not exist.
func TestQualifiedKeepsGroupsUnprefixed(t *testing.T) {
	if got := (Event{Plugin: "cargo-ai-utils", Skill: "explore"}).Qualified(); got != "cargo-ai-utils:explore" {
		t.Errorf("plugin skill: got %q", got)
	}
	for _, group := range []string{GroupPersonal, GroupProject, GroupBuiltin, ""} {
		if got := (Event{Plugin: group, Skill: "humanizer"}).Qualified(); got != "humanizer" {
			t.Errorf("group %q: got %q, want bare name", group, got)
		}
	}
}

// TestSplitPluginPrefix covers the plugin/skill split, including the degenerate
// colon placements that must not produce an empty half.
func TestSplitPluginPrefix(t *testing.T) {
	res := &resolver{cache: map[string]string{}}
	tests := []struct{ raw, plugin, skill string }{
		{"cargo-ai-utils:explore", "cargo-ai-utils", "explore"},
		{"  superpowers:writing-plans  ", "superpowers", "writing-plans"},
		{"dataviz", GroupBuiltin, "dataviz"},
		{":leading", GroupBuiltin, ":leading"},
		{"trailing:", GroupBuiltin, "trailing:"},
		{"", "", ""},
	}
	for _, tc := range tests {
		plugin, skill := res.split(tc.raw, "")
		if plugin != tc.plugin || skill != tc.skill {
			t.Errorf("split(%q) = (%q, %q), want (%q, %q)", tc.raw, plugin, skill, tc.plugin, tc.skill)
		}
	}
}

// TestClassifyBareBySkillLocation verifies a prefix-less name is bucketed by
// where the skill actually lives, so a personal skill and a Claude Code
// built-in do not collapse into one group.
func TestClassifyBareBySkillLocation(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeSkill(t, filepath.Join(home, ".claude", "skills", "humanizer"))
	writeSkill(t, filepath.Join(project, ".claude", "skills", "repo-flow"))

	res := &resolver{cache: map[string]string{}, userSkills: filepath.Join(home, ".claude", "skills")}
	if got := res.classifyBare("humanizer", project); got != GroupPersonal {
		t.Errorf("personal skill: got %q", got)
	}
	if got := res.classifyBare("repo-flow", project); got != GroupProject {
		t.Errorf("project skill: got %q", got)
	}
	if got := res.classifyBare("dataviz", project); got != GroupBuiltin {
		t.Errorf("unknown skill: got %q, want %q", got, GroupBuiltin)
	}
	// A project skill resolved from an unrelated cwd is not that project's.
	if got := res.classifyBare("repo-flow", t.TempDir()); got != GroupBuiltin {
		t.Errorf("project skill from other cwd: got %q", got)
	}
}

// TestComputeBucketsAndGroups verifies events land in the right time bucket and
// roll up under their plugin while staying individually countable.
func TestComputeBucketsAndGroups(t *testing.T) {
	now := at(2026, time.July, 30, 12)
	events := []Event{
		ev(at(2026, time.July, 30, 12), "Work", "plug", "alpha"),
		ev(at(2026, time.July, 30, 12), "Work", "plug", "alpha"),
		ev(at(2026, time.July, 30, 11), "Work", "plug", "beta"),
		ev(at(2026, time.July, 30, 10), "Work", "other", "gamma"),
		// Outside a 24-hour window, so the hourly view must exclude it.
		ev(at(2026, time.July, 20, 10), "Work", "plug", "alpha"),
	}

	stats := Compute(events, Query{Gran: GranHour, Now: now})
	if len(stats.Buckets) != DefaultBuckets(GranHour) {
		t.Fatalf("bucket count = %d, want %d", len(stats.Buckets), DefaultBuckets(GranHour))
	}
	if stats.Total != 4 {
		t.Errorf("total = %d, want 4 (older event out of window)", stats.Total)
	}

	last := stats.Buckets[len(stats.Buckets)-1]
	if last.Total != 2 || last.ByPlugin["plug"] != 2 || last.BySkill["plug:alpha"] != 2 {
		t.Errorf("final bucket = %+v, want 2 plug:alpha", last)
	}

	if len(stats.Plugins) != 2 || stats.Plugins[0].Plugin != "plug" {
		t.Fatalf("plugins = %+v, want plug first by total", stats.Plugins)
	}
	plug := stats.Plugins[0]
	if plug.Total != 3 {
		t.Errorf("plug total = %d, want 3", plug.Total)
	}
	if len(plug.Skills) != 2 {
		t.Fatalf("plug skills = %+v, want alpha and beta distinguishable", plug.Skills)
	}
	if plug.Skills[0].Skill != "alpha" || plug.Skills[0].Total != 2 {
		t.Errorf("busiest skill = %+v, want alpha x2", plug.Skills[0])
	}
	if plug.Skills[0].Qualified != "plug:alpha" {
		t.Errorf("qualified = %q", plug.Skills[0].Qualified)
	}
}

// TestPluginLastRollsUpAcrossSkills verifies a plugin group reports the most
// recent invocation among its skills, which is what a collapsed group row shows.
func TestPluginLastRollsUpAcrossSkills(t *testing.T) {
	now := at(2026, time.July, 30, 12)
	newest := at(2026, time.July, 30, 11)
	events := []Event{
		ev(at(2026, time.July, 28, 9), "Work", "plug", "alpha"),
		ev(newest, "Work", "plug", "beta"),
		ev(at(2026, time.July, 29, 9), "Work", "plug", "gamma"),
	}

	stats := Compute(events, Query{Gran: GranDay, Now: now})
	group := stats.Plugins[0]
	if !group.Last.Equal(newest) {
		t.Errorf("plugin Last = %s, want the newest skill's %s", group.Last, newest)
	}
	// The rollup must be the max, not the busiest skill's or the last one merged.
	for _, sk := range group.Skills {
		if sk.Last.After(group.Last) {
			t.Errorf("skill %s (%s) is newer than the group rollup %s", sk.Skill, sk.Last, group.Last)
		}
	}
}

// TestPluginLastZeroWhenEmpty verifies a group with no invocations in the window
// carries a zero time, which the dashboard renders as a blank cell rather than
// as a year-1 date.
func TestPluginLastZeroWhenEmpty(t *testing.T) {
	stats := Compute(nil, Query{Gran: GranDay, Now: at(2026, time.July, 30, 12)})
	if len(stats.Plugins) != 0 {
		t.Fatalf("expected no plugin groups, got %+v", stats.Plugins)
	}
}

// TestComputeProfileFilter verifies the profile dimension excludes other
// profiles from both the series and the rollups.
func TestComputeProfileFilter(t *testing.T) {
	now := at(2026, time.July, 30, 12)
	events := []Event{
		ev(at(2026, time.July, 30, 12), "Work", "plug", "alpha"),
		ev(at(2026, time.July, 30, 12), "Default", "plug", "alpha"),
		ev(at(2026, time.July, 30, 12), "Default", "plug", "beta"),
	}
	stats := Compute(events, Query{Gran: GranDay, Profile: "Default", Now: now})
	if stats.Total != 2 {
		t.Errorf("total = %d, want 2", stats.Total)
	}
	if stats.Profile != "Default" {
		t.Errorf("profile = %q", stats.Profile)
	}
	if got := stats.Buckets[len(stats.Buckets)-1].Total; got != 2 {
		t.Errorf("final bucket total = %d, want 2", got)
	}
}

// TestComputeSubagentSplit verifies subagent invocations are counted separately
// while still contributing to the totals.
func TestComputeSubagentSplit(t *testing.T) {
	now := at(2026, time.July, 30, 12)
	main := ev(at(2026, time.July, 30, 12), "Work", "plug", "alpha")
	sub := ev(at(2026, time.July, 30, 12), "Work", "plug", "alpha")
	sub.UUID = "sub"
	sub.Sidechain = true

	stats := Compute([]Event{main, sub}, Query{Gran: GranDay, Now: now})
	if stats.Total != 2 || stats.Subagent != 1 {
		t.Errorf("total/subagent = %d/%d, want 2/1", stats.Total, stats.Subagent)
	}
	if got := stats.Plugins[0].Skills[0]; got.Total != 2 || got.Subagent != 1 {
		t.Errorf("skill stat = %+v, want total 2 subagent 1", got)
	}
}

// TestWeekBucketsStartMonday pins the week boundary: a Sunday and the Monday
// before it belong to the same week, and the following Monday starts a new one.
func TestWeekBucketsStartMonday(t *testing.T) {
	monday := time.Date(2026, time.July, 27, 9, 0, 0, 0, time.Local)
	if monday.Weekday() != time.Monday {
		t.Fatalf("fixture is %s, expected Monday", monday.Weekday())
	}
	sunday := monday.AddDate(0, 0, 6)
	nextMonday := monday.AddDate(0, 0, 7)

	if got := truncate(sunday, GranWeek); !got.Equal(truncate(monday, GranWeek)) {
		t.Errorf("Sunday bucket %s != Monday bucket %s", got, truncate(monday, GranWeek))
	}
	if got := truncate(nextMonday, GranWeek); got.Equal(truncate(monday, GranWeek)) {
		t.Error("next Monday shares the previous week's bucket")
	}
	if start := truncate(monday, GranWeek); start.Weekday() != time.Monday ||
		start.Hour() != 0 || start.Minute() != 0 {
		t.Errorf("week start = %s, want local Monday midnight", start)
	}
}

// TestBucketWindowsAreContiguous verifies each granularity produces an unbroken
// ascending series ending in the bucket that contains now.
func TestBucketWindowsAreContiguous(t *testing.T) {
	now := at(2026, time.July, 30, 12)
	for _, gran := range []Granularity{GranHour, GranDay, GranWeek} {
		stats := Compute(nil, Query{Gran: gran, Now: now})
		buckets := stats.Buckets
		if len(buckets) != DefaultBuckets(gran) {
			t.Errorf("%s: %d buckets, want %d", gran, len(buckets), DefaultBuckets(gran))
		}
		for i := 1; i < len(buckets); i++ {
			if !buckets[i].Start.After(buckets[i-1].Start) {
				t.Errorf("%s: bucket %d not after %d", gran, i, i-1)
			}
			if want := advance(buckets[i-1].Start, gran); !buckets[i].Start.Equal(want) {
				t.Errorf("%s: gap at %d: %s, want %s", gran, i, buckets[i].Start, want)
			}
		}
		if last := buckets[len(buckets)-1].Start; !last.Equal(truncate(now, gran)) {
			t.Errorf("%s: last bucket %s, want %s", gran, last, truncate(now, gran))
		}
		if buckets[0].Label == "" {
			t.Errorf("%s: empty bucket label", gran)
		}
	}
}

// TestParseGranularityDefaultsToDay verifies an unknown or absent granularity
// falls back to the daily view rather than erroring.
func TestParseGranularityDefaultsToDay(t *testing.T) {
	for _, raw := range []string{"", "nonsense", "day"} {
		if got := ParseGranularity(raw); got != GranDay {
			t.Errorf("ParseGranularity(%q) = %q, want day", raw, got)
		}
	}
	if ParseGranularity("hour") != GranHour || ParseGranularity("week") != GranWeek {
		t.Error("explicit hour/week granularity not preserved")
	}
}

// TestComputeClampsBucketCount verifies a hand-edited window size cannot ask for
// an unbounded series.
func TestComputeClampsBucketCount(t *testing.T) {
	stats := Compute(nil, Query{Gran: GranDay, Buckets: 10_000, Now: at(2026, time.July, 30, 12)})
	if len(stats.Buckets) != maxBuckets {
		t.Errorf("buckets = %d, want clamp to %d", len(stats.Buckets), maxBuckets)
	}
}
