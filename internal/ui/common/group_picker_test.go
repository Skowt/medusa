package common

import "testing"

func TestNewGroupPicker_OptionsOrder(t *testing.T) {
	d := NewGroupPicker("id", []string{"bugs", "shipping"}, "")
	gotOpts := d.options
	want := []string{UngroupedOption, "bugs", "shipping", NewGroupOption}
	if len(gotOpts) != len(want) {
		t.Fatalf("len=%d want=%d", len(gotOpts), len(want))
	}
	for i := range want {
		if gotOpts[i] != want[i] {
			t.Errorf("options[%d]=%q want %q", i, gotOpts[i], want[i])
		}
	}
}

func TestNewGroupPicker_CursorStartsOnCurrent(t *testing.T) {
	d := NewGroupPicker("id", []string{"bugs", "shipping"}, "shipping")
	if d.cursor != 2 {
		t.Errorf("cursor=%d want 2 (shipping)", d.cursor)
	}
}

func TestNewGroupPicker_CursorStartsOnUngroupedWhenEmpty(t *testing.T) {
	d := NewGroupPicker("id", []string{"bugs"}, "")
	if d.cursor != 0 {
		t.Errorf("cursor=%d want 0 (Ungrouped)", d.cursor)
	}
}
