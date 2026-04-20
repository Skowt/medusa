package data

import (
	"sort"
	"strings"
)

// RepoKeyFor returns the stable scope key used to group workspaces by their repo set.
// Single-repo workspaces use the repo name; multi-repo workspaces use the sorted
// comma-joined list of repo names. Workspaces with no repos use "other".
// This key is used both for dashboard repo-section headers and for scoping user-defined groups.
func RepoKeyFor(w *Workspace) string {
	if w == nil || len(w.Repos) == 0 {
		return "other"
	}
	names := make([]string, len(w.Repos))
	for i, r := range w.Repos {
		names[i] = r.Name
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// RepoLabelFor returns the display label for a repo scope key, truncated to 15 chars.
// The label is derived from the same sorted repo names used in RepoKeyFor and may differ
// from the key only by truncation — callers that need a stable identifier must use RepoKeyFor.
func RepoLabelFor(w *Workspace) string {
	if w == nil || len(w.Repos) == 0 {
		return "other"
	}
	names := make([]string, len(w.Repos))
	for i, r := range w.Repos {
		names[i] = r.Name
	}
	sort.Strings(names)
	label := strings.Join(names, ", ")
	if len(label) > 15 {
		label = label[:15] + "..."
	}
	return label
}
