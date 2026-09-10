package gitreview

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

// TestTheOpeningLiveEventCarriesTheWholeState guards the class of bug that
// building an Event by hand creates.
//
// The page cannot tell an absent field from a false one, so an event that omits
// the read ticks or the send mode does not leave them alone -- it clears them.
// Built by hand, this event wiped both off a page that had just loaded them
// correctly from the snapshot endpoint.
func TestTheOpeningLiveEventCarriesTheWholeState(t *testing.T) {
	ts, _, session, _ := harness(t)

	session.SetAutoSend(true)
	if !session.SetViewed("keep.txt", true) {
		t.Fatal("could not mark a file read")
	}

	res, err := ts.Client().Get(ts.URL + "/s/" + session.ID() + "/api/live")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	reader := bufio.NewReader(res.Body)
	var payload string
	for i := 0; i < 20 && payload == ""; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			payload = strings.TrimSpace(after)
		}
	}
	if payload == "" {
		t.Fatal("no event arrived on the live stream")
	}

	var ev Event
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		t.Fatalf("decoding %q: %v", payload, err)
	}
	if !ev.AutoSend {
		t.Error("the opening event reported auto-send off, which would reset the toggle")
	}
	if len(ev.Viewed) != 1 || ev.Viewed[0] != "keep.txt" {
		t.Errorf("the opening event carried viewed = %v, which would clear the ticks", ev.Viewed)
	}
	if ev.Snapshot == nil {
		t.Error("the opening event carried no snapshot")
	}
}
