// Package gitreview serves a local, live web view of a workspace's git changes
// and carries the review comments the user writes there back to the agent that
// made them.
//
// It exists as an HTTP surface rather than a TUI pane for one reason: a diff
// review wants a browser's width, mouse selection and text input, and none of
// those are things a terminal pane does well. The page is served on an
// ephemeral loopback port, so it is never reachable off the machine.
package gitreview

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Skowt/medusa/internal/git"
)

// Scope selects which changes a review covers.
//
// Working is the default because it is the work the user is looking at when they
// open a review: what the agent has just done and has not committed. The branch
// view is a step back from that -- it also covers commits made earlier in the
// task, which is the right question later but buries the fresh changes on the
// first look.
type Scope string

const (
	// ScopeBranch covers everything since the base branch — commits on this
	// branch plus anything still uncommitted.
	ScopeBranch Scope = "branch"
	// ScopeWorking covers uncommitted changes only, staged or not.
	ScopeWorking Scope = "working"
)

// Theme is the review page's colour scheme.
//
// The empty value is not a missing answer, it is an answer: follow whatever the
// reader's OS is set to, which is what the page did before there was a choice
// and what a fresh config still asks for.
type Theme string

const (
	// ThemeSystem follows the OS setting.
	ThemeSystem Theme = ""
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// ParseTheme maps a stored or posted value onto a theme, treating anything it
// does not recognise as "follow the OS".
//
// Refusing instead would leave a hand-edited config with no theme at all, and
// falling back to a fixed one would override a preference the reader expressed
// in their system settings.
func ParseTheme(raw string) Theme {
	switch Theme(raw) {
	case ThemeLight:
		return ThemeLight
	case ThemeDark:
		return ThemeDark
	default:
		return ThemeSystem
	}
}

// ParseScope maps a query parameter to a scope, defaulting to the uncommitted
// view.
func ParseScope(raw string) Scope {
	if Scope(raw) == ScopeBranch {
		return ScopeBranch
	}
	return ScopeWorking
}

// Snapshot is the whole reviewable state of a workspace at one moment. The page
// renders it directly, and the digest is what tells a live client that the
// changes underneath it have moved.
type Snapshot struct {
	Root      string `json:"root"`
	Workspace string `json:"workspace"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	// BaseRev and BaseSubject name the commit the diff is actually measured
	// from. The base branch alone does not: it is derived rather than chosen,
	// and a rebase moves it under the reader mid-session, so a review labelled
	// only "main" gives them no way to tell which fork point they are looking at.
	BaseRev     string `json:"baseRev,omitempty"`
	BaseSubject string `json:"baseSubject,omitempty"`
	Scope       Scope  `json:"scope"`
	// Agent is stamped on by the Session after Build, which is a pure git
	// function and has no business knowing about agents.
	Agent       AgentProfile `json:"agent"`
	Files       []File       `json:"files"`
	Digest      string       `json:"digest"`
	GeneratedAt time.Time    `json:"generatedAt"`
	// Error reports a failure to read the repository at all. Per-file failures
	// ride on the file instead, so one unreadable file does not blank the view.
	Error string `json:"error,omitempty"`
	// Notice is a non-fatal caveat about the snapshot itself, such as having
	// stopped short of enumerating every new file.
	Notice string `json:"notice,omitempty"`
}

// File is one changed path and its parsed diff.
type File struct {
	// Digest fingerprints this file's diff alone, and two things hang off it.
	// A "viewed" mark is pinned to it, so the mark clears the moment the agent
	// touches the file again -- a page still ticking a file that has since been
	// rewritten is worse than one that never offered the tick. The page also
	// uses it to drop the context it has expanded around that file's hunks,
	// which is stale for the same reason and would otherwise show lines the
	// file no longer contains.
	Digest    string `json:"digest"`
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Binary    bool   `json:"binary"`
	Large     bool   `json:"large"`
	Untracked bool   `json:"untracked"`
	Error     string `json:"error,omitempty"`
	Hunks     []Hunk `json:"hunks"`
}

// Hunk is a contiguous run of diff lines under one @@ header.
type Hunk struct {
	Header string `json:"header"`
	Lines  []Line `json:"lines"`
}

// Line is one row of a diff, carrying the position it occupies in each side of
// the file. Old and New are what a comment anchors to: the rendered row index
// names no real place in the file, so it cannot be reported to an agent.
type Line struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
	Old     int    `json:"old,omitempty"`
	New     int    `json:"new,omitempty"`
}

// Line kinds, as the page sees them.
const (
	LineContext = "context"
	LineAdd     = "add"
	LineDel     = "del"
)

// Build reads the repository and returns everything the review page needs.
//
// It runs git several times over, so it must not be called on a UI thread.
func Build(root, workspace string, scope Scope) Snapshot {
	snap := Snapshot{
		Root:        root,
		Workspace:   workspace,
		Scope:       scope,
		GeneratedAt: time.Now(),
		Files:       []File{},
	}
	if root == "" {
		snap.Error = "no workspace root"
		return snap
	}

	snap.Branch, _ = git.GetCurrentBranch(root)
	base := resolveBase(root, scope)
	snap.Base, snap.BaseRev, snap.BaseSubject = base.label, base.short, base.subject

	changes, err := changedFiles(root, base.rev)
	if err != nil {
		snap.Error = err.Error()
		return snap
	}
	snap.Notice = changes.notice
	for _, entry := range changes.entries {
		f := buildFile(root, base.rev, entry, changes.diffs)
		f.Digest = fileDigest(f)
		snap.Files = append(snap.Files, f)
	}
	snap.Digest = digest(snap.BaseRev, snap.Files)
	return snap
}

// fileDigest fingerprints one file's diff.
//
// It covers the content of every hunk, not just the status and line counts: an
// agent rewriting a line in place leaves both of those identical, and neither a
// page that does not repaint for that nor a viewed mark that survives it is
// telling the user the truth.
func fileDigest(f File) string {
	h := sha256.New()
	_, _ = h.Write([]byte(f.Path + "\x00" + f.Status + "\x00"))
	for _, hunk := range f.Hunks {
		_, _ = h.Write([]byte(hunk.Header + "\x00"))
		for _, line := range hunk.Lines {
			_, _ = h.Write([]byte(line.Kind + line.Content + "\x00"))
		}
	}
	_, _ = h.Write([]byte(f.Error + "\x01"))
	return hex.EncodeToString(h.Sum(nil))
}

// digest fingerprints the whole diff so a live client can be told "nothing
// changed" without re-sending it.
//
// It is built from the per-file digests rather than from the files again, so the
// two can never disagree about what counts as a change.
//
// The base is part of it. A rebase can move the fork point while leaving the
// resulting diff byte-for-byte identical, and a page that does not repaint for
// that goes on naming a commit the review is no longer measured from.
func digest(baseRev string, files []File) string {
	h := sha256.New()
	_, _ = h.Write([]byte(baseRev + "\x03"))
	for _, f := range files {
		_, _ = h.Write([]byte(f.Digest + "\x02"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hunksFrom groups a parsed diff's flat line list into hunks.
//
// The preamble (`diff --git`, `index`, `---`, `+++`) is dropped: it names the
// file, which the page already shows in its navigator, and rendering it as diff
// rows puts four unreviewable lines above every file.
func hunksFrom(lines []git.DiffLine) []Hunk {
	var hunks []Hunk
	for _, line := range lines {
		if strings.HasPrefix(line.Content, "@@") {
			hunks = append(hunks, Hunk{Header: line.Content})
			continue
		}
		if len(hunks) == 0 {
			continue // preamble, before any hunk header
		}
		if line.Kind == git.DiffLineHeader {
			continue
		}
		// strings.Split leaves an empty row for a diff's trailing newline. It
		// is not a line of the file and must not become a reviewable row.
		if line.Content == "" {
			continue
		}
		cur := &hunks[len(hunks)-1]
		cur.Lines = append(cur.Lines, Line{
			Kind:    lineKind(line.Kind),
			Content: line.Content,
			Old:     line.OldLine,
			New:     line.NewLine,
		})
	}
	return hunks
}

func lineKind(k git.DiffLineKind) string {
	switch k {
	case git.DiffLineAdd:
		return LineAdd
	case git.DiffLineDelete:
		return LineDel
	default:
		return LineContext
	}
}
