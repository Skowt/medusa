package hooks

import (
	"testing"
	"time"
)

// TestParseHookTS verifies timestamps are normalized to the same timescale
// regardless of the resolution the emitting hook used, so second-resolution
// (legacy / %N-less date) and nanosecond events are directly comparable.
func TestParseHookTS(t *testing.T) {
	want := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name string
		ts   int64
		want time.Time
	}{
		{"seconds", 1_700_000_000, want},
		{"milliseconds", 1_700_000_000_000, want},
		{"microseconds", 1_700_000_000_000_000, want},
		{"nanoseconds", 1_700_000_000_000_000_000, want},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseHookTS(c.ts); !got.Equal(c.want) {
				t.Errorf("parseHookTS(%d) = %v, want %v", c.ts, got, c.want)
			}
		})
	}

	// Sub-second precision must survive for nanosecond input, so two events in
	// the same second are still ordered — the whole point of the resolution bump.
	a := parseHookTS(1_700_000_000_100_000_000)
	b := parseHookTS(1_700_000_000_900_000_000)
	if !a.Before(b) {
		t.Errorf("sub-second ordering lost: %v not before %v", a, b)
	}
}
