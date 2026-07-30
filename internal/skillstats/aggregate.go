package skillstats

import (
	"fmt"
	"sort"
	"time"
)

// Granularity is a bucket width for the dashboard, each with the default window
// the user sees when they pick it.
type Granularity string

const (
	GranHour Granularity = "hour"
	GranDay  Granularity = "day"
	GranWeek Granularity = "week"
)

// defaultBuckets is the window each granularity opens with: a full day of
// hours, a month of days, a quarter of weeks. Each spans enough to show a trend
// without compressing the bars into noise.
var defaultBuckets = map[Granularity]int{
	GranHour: 24,
	GranDay:  30,
	GranWeek: 12,
}

// maxBuckets caps a caller-supplied window so a hand-edited URL cannot ask for
// an unbounded number of buckets.
const maxBuckets = 400

// ParseGranularity maps a request value to a granularity, defaulting to daily.
func ParseGranularity(s string) Granularity {
	switch Granularity(s) {
	case GranHour:
		return GranHour
	case GranWeek:
		return GranWeek
	default:
		return GranDay
	}
}

// DefaultBuckets returns the default window size for a granularity.
func DefaultBuckets(g Granularity) int { return defaultBuckets[g] }

// Query selects and shapes the events a Stats call reports on.
type Query struct {
	Gran Granularity
	// Buckets is the window length in units of Gran; zero means the default.
	Buckets int
	// Profile filters to one profile; empty includes all.
	Profile string
	// Now anchors the window's final bucket. Zero means time.Now().
	Now time.Time
}

// Stats is the dashboard payload: a time series bucketed at one granularity,
// plus per-plugin totals with their individual skills.
type Stats struct {
	Gran     Granularity  `json:"gran"`
	Profile  string       `json:"profile"`
	From     time.Time    `json:"from"`
	To       time.Time    `json:"to"`
	Buckets  []Bucket     `json:"buckets"`
	Plugins  []PluginStat `json:"plugins"`
	Total    int          `json:"total"`
	Subagent int          `json:"subagent"`
}

// Bucket is one time slot of the series. ByPlugin drives the stacked bars and
// BySkill the drill-down, both keyed so a plugin's skills stay individually
// distinguishable inside its group.
type Bucket struct {
	Start    time.Time      `json:"start"`
	Label    string         `json:"label"`
	Total    int            `json:"total"`
	ByPlugin map[string]int `json:"byPlugin"`
	BySkill  map[string]int `json:"bySkill"`
}

// PluginStat totals one plugin (or bare-name group) over the window. Last is
// the most recent invocation across the group's skills, so a collapsed group
// still reports when it was last used.
type PluginStat struct {
	Plugin   string      `json:"plugin"`
	Total    int         `json:"total"`
	Subagent int         `json:"subagent"`
	Last     time.Time   `json:"last"`
	Skills   []SkillStat `json:"skills"`
}

// SkillStat totals one skill over the window.
type SkillStat struct {
	Skill     string    `json:"skill"`
	Qualified string    `json:"qualified"`
	Total     int       `json:"total"`
	Subagent  int       `json:"subagent"`
	Last      time.Time `json:"last"`
}

// Compute buckets events into the series and totals the dashboard renders.
// Events outside the window, or from another profile, are excluded.
func Compute(events []Event, q Query) Stats {
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}
	count := q.Buckets
	if count <= 0 {
		count = defaultBuckets[q.Gran]
	}
	if count > maxBuckets {
		count = maxBuckets
	}

	starts := bucketStarts(now, q.Gran, count)
	stats := Stats{
		Gran:    q.Gran,
		Profile: q.Profile,
		From:    starts[0],
		To:      advance(starts[len(starts)-1], q.Gran),
		Buckets: make([]Bucket, len(starts)),
	}
	index := make(map[time.Time]int, len(starts))
	for i, start := range starts {
		stats.Buckets[i] = Bucket{
			Start:    start,
			Label:    label(start, q.Gran),
			ByPlugin: map[string]int{},
			BySkill:  map[string]int{},
		}
		index[start] = i
	}

	// plugin -> qualified skill -> running stat, so a skill's identity survives
	// aggregation while still rolling up under its plugin.
	groups := map[string]map[string]*SkillStat{}

	for _, e := range events {
		if q.Profile != "" && e.Profile != q.Profile {
			continue
		}
		start := truncate(e.TS, q.Gran)
		i, ok := index[start]
		if !ok {
			continue
		}
		qualified := e.Qualified()
		b := &stats.Buckets[i]
		b.Total++
		b.ByPlugin[e.Plugin]++
		b.BySkill[qualified]++
		stats.Total++
		if e.Sidechain {
			stats.Subagent++
		}

		skills, ok := groups[e.Plugin]
		if !ok {
			skills = map[string]*SkillStat{}
			groups[e.Plugin] = skills
		}
		stat, ok := skills[qualified]
		if !ok {
			stat = &SkillStat{Skill: e.Skill, Qualified: qualified}
			skills[qualified] = stat
		}
		stat.Total++
		if e.Sidechain {
			stat.Subagent++
		}
		if e.TS.After(stat.Last) {
			stat.Last = e.TS
		}
	}

	stats.Plugins = flattenGroups(groups)
	return stats
}

// flattenGroups turns the accumulator into the sorted plugin list the dashboard
// renders: plugins by descending total, and each plugin's skills likewise, so
// the heaviest users of skills read off the top.
func flattenGroups(groups map[string]map[string]*SkillStat) []PluginStat {
	out := make([]PluginStat, 0, len(groups))
	for plugin, skills := range groups {
		entry := PluginStat{Plugin: plugin, Skills: make([]SkillStat, 0, len(skills))}
		for _, stat := range skills {
			entry.Total += stat.Total
			entry.Subagent += stat.Subagent
			if stat.Last.After(entry.Last) {
				entry.Last = stat.Last
			}
			entry.Skills = append(entry.Skills, *stat)
		}
		sort.Slice(entry.Skills, func(i, j int) bool {
			if entry.Skills[i].Total != entry.Skills[j].Total {
				return entry.Skills[i].Total > entry.Skills[j].Total
			}
			return entry.Skills[i].Qualified < entry.Skills[j].Qualified
		})
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Plugin < out[j].Plugin
	})
	return out
}

// bucketStarts returns count bucket start times ending with the one containing
// now, oldest first.
func bucketStarts(now time.Time, gran Granularity, count int) []time.Time {
	last := truncate(now, gran)
	starts := make([]time.Time, count)
	for i := range starts {
		starts[i] = rewind(last, gran, count-1-i)
	}
	return starts
}

// truncate snaps a time down to its bucket start in the local zone. Local
// rather than UTC is deliberate: a dashboard read by a person should break days
// and weeks where their day and week actually break.
func truncate(t time.Time, gran Granularity) time.Time {
	t = t.Local()
	switch gran {
	case GranHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	case GranWeek:
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		// ISO weeks start Monday; Go's Sunday==0 needs the shift.
		offset := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
}

// advance returns the start of the bucket after t.
func advance(t time.Time, gran Granularity) time.Time { return rewind(t, gran, -1) }

// rewind steps n buckets back from t. Calendar arithmetic (AddDate) is used for
// day and week so a DST transition shifts the wall-clock boundary instead of
// sliding every later bucket by an hour.
func rewind(t time.Time, gran Granularity, n int) time.Time {
	switch gran {
	case GranHour:
		return t.Add(-time.Duration(n) * time.Hour)
	case GranWeek:
		return t.AddDate(0, 0, -7*n)
	default:
		return t.AddDate(0, 0, -n)
	}
}

// label renders a bucket's axis label. The server formats it so the browser
// never has to reconstruct the local zone or the week boundary.
func label(start time.Time, gran Granularity) string {
	switch gran {
	case GranHour:
		return start.Format("15:04")
	case GranWeek:
		_, week := start.ISOWeek()
		return fmt.Sprintf("W%02d %s", week, start.Format("Jan 2"))
	default:
		return start.Format("Jan 2")
	}
}
