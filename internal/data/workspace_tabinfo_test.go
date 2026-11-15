package data

import (
	"encoding/json"
	"testing"
)

func TestTabInfoLaunchFlagsRoundTrip(t *testing.T) {
	in := TabInfo{Assistant: "claude", Name: "claude", Fullscreen: true, Chrome: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out TabInfo
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Fullscreen || !out.Chrome {
		t.Errorf("launch flags did not round-trip: %s", b)
	}

	// Legacy JSON without the field defaults to false (fallback).
	var legacy TabInfo
	if err := json.Unmarshal([]byte(`{"assistant":"claude","name":"claude"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Fullscreen || legacy.Chrome {
		t.Errorf("legacy tab must default to non-fullscreen, Chrome off")
	}
}
