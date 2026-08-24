package dashboard

import (
	"fmt"
	"math/rand/v2"
)

// Group labels generated for a workspace dropped on "New group". A drop has to
// name the group there and then — the label *is* the group, since a group is
// nothing but the label its members share — so the name is generated rather
// than prompted for, and renamed later with `r` on the header if the user cares.
var (
	groupAdjectives = []string{
		"brave", "brisk", "cosmic", "dapper", "eager", "feral", "gilded",
		"humble", "jolly", "keen", "lucid", "mellow", "nimble", "plucky",
		"quiet", "rowdy", "sly", "snug", "spry", "stout", "sunny", "tidy",
		"vivid", "wily", "zesty",
	}
	groupNouns = []string{
		"otter", "falcon", "badger", "cactus", "comet", "ember", "ferret",
		"gecko", "heron", "ibex", "jackal", "kestrel", "lemur", "marmot",
		"newt", "osprey", "puffin", "quokka", "raven", "shrike", "tapir",
		"urchin", "walrus", "yak", "zebu",
	}
)

// newGroupName returns a two-word label that is not already in use.
func newGroupName(taken map[string]bool) string {
	var last string
	for range 64 {
		last = groupAdjectives[rand.IntN(len(groupAdjectives))] + "-" + groupNouns[rand.IntN(len(groupNouns))]
		if !taken[last] {
			return last
		}
	}
	// Every draw collided, which takes hundreds of groups. Number the last one
	// rather than loop forever on a shrinking pool.
	for n := 2; ; n++ {
		numbered := fmt.Sprintf("%s-%d", last, n)
		if !taken[numbered] {
			return numbered
		}
	}
}

// groupLabels returns every label currently in use, archived workspaces
// included: unarchiving one would bring its group back, so a generated name
// must not land on it.
func (m *Model) groupLabels() map[string]bool {
	taken := make(map[string]bool)
	for _, ws := range m.workspaces {
		if ws != nil && ws.Group != "" {
			taken[ws.Group] = true
		}
	}
	return taken
}
