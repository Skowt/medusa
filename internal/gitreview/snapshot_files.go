package gitreview

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Skowt/medusa/internal/git"
)

// fileEntry is one path git reported as changed, before its diff is read.
type fileEntry struct {
	path      string
	oldPath   string
	status    string
	untracked bool
}

// maxUntrackedFiles caps how many brand-new files one review will enumerate.
//
// The cap exists because untracked enumeration is the one part of a snapshot
// whose cost is not bounded by the size of the change: a repo with a large
// un-ignored directory (a vendored cache, a build output tree someone forgot to
// ignore) can answer with tens of thousands of paths, and every one of them
// would then get its own git diff on every poll.
const maxUntrackedFiles = 500

// changedFiles lists every path that differs from rev, plus untracked files.
//
// The diff is against the working tree rather than `rev...HEAD`, so a file the
// agent has edited but not committed appears alongside the ones it committed.
// Untracked files are not in any diff and have to be asked for separately.
func changedFiles(root, rev string) (changeSet, error) {
	out, err := git.RunGitRaw(root, "--no-optional-locks", "diff", "--name-status", "-z", "--find-renames", rev)
	if err != nil {
		return changeSet{}, err
	}
	entries := parseNameStatus(string(out))

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.path] = true
	}
	untracked, notice := untrackedFiles(root)
	for _, path := range untracked {
		if seen[path] {
			continue
		}
		entries = append(entries, fileEntry{path: path, status: "untracked", untracked: true})
	}

	sortEntries(entries)
	return changeSet{entries: entries, diffs: bulkDiff(root, rev, entries), notice: notice}, nil
}

// untrackedFiles lists brand-new files individually, and reports a notice when
// it had to stop short.
//
// This uses `ls-files --others` rather than git status, which is what the rest
// of Medusa reads. Status runs with --untracked-files=normal and therefore stops
// at untracked-directory boundaries: a whole new directory arrives as the single
// entry "internal/thing/" instead of the files in it. For the dashboard's change
// indicator that is the right trade -- it only needs a count -- but for a review
// it is fatal twice over. The directory row has no diff to show, so the files
// are unreviewable; and because the row's content never changes, editing
// anything inside it does not move the snapshot digest, so an open page never
// repaints. An agent creating a new package is the common case, not a corner
// one.
//
// --exclude-standard keeps .gitignore respected, which is what stops this from
// walking build output and dependency trees in the first place.
func untrackedFiles(root string) ([]string, string) {
	out, err := git.RunGitRaw(root, "--no-optional-locks", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, ""
	}
	var paths []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" {
			continue
		}
		if len(paths) >= maxUntrackedFiles {
			return paths, "Only the first " + strconv.Itoa(maxUntrackedFiles) +
				" new files are shown. Commit or ignore the rest to review them."
		}
		paths = append(paths, path)
	}
	return paths, ""
}

// parseNameStatus reads `git diff --name-status -z` output.
//
// The -z form is what makes paths with spaces, quotes or non-UTF-8 bytes safe:
// the newline-delimited form quotes and escapes those, so a review would show
// an escaped path that matches nothing on disk. Renames and copies spend two
// fields on the path instead of one, which is why this cannot be a plain pairwise
// walk over the records.
func parseNameStatus(raw string) []fileEntry {
	records := strings.Split(raw, "\x00")
	var entries []fileEntry
	for i := 0; i < len(records); i++ {
		code := records[i]
		if code == "" {
			continue
		}
		letter := code[0]
		if letter == 'R' || letter == 'C' {
			if i+2 >= len(records) {
				break
			}
			entries = append(entries, fileEntry{
				oldPath: records[i+1],
				path:    records[i+2],
				status:  statusLabel(letter),
			})
			i += 2
			continue
		}
		if i+1 >= len(records) {
			break
		}
		entries = append(entries, fileEntry{
			path:   records[i+1],
			status: statusLabel(letter),
		})
		i++
	}
	return entries
}

func statusLabel(code byte) string {
	switch code {
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "typechange"
	default:
		return "modified"
	}
}

// sortEntries orders files by directory then name, which is the order a file
// navigator has to show them in anyway.
func sortEntries(entries []fileEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].path < entries[j-1].path; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// buildFile turns one changed path into reviewable hunks, preferring the patch
// already split out of the bulk diff.
func buildFile(root, rev string, entry fileEntry, diffs map[string]string) File {
	f := File{
		Path:      entry.path,
		OldPath:   entry.oldPath,
		Status:    entry.status,
		Untracked: entry.untracked,
		Hunks:     []Hunk{},
	}

	var result *git.DiffResult
	switch {
	case entry.untracked:
		result = untrackedDiff(root, entry.path)
	case diffs[entry.path] != "":
		result = git.ParseDiff(entry.path, diffs[entry.path])
	default:
		// Either the bulk diff could not be attributed, or this path's header
		// was quoted. One extra subprocess is the right price for being sure.
		var err error
		result, err = diffAgainst(root, rev, entry)
		if err != nil {
			f.Error = err.Error()
			return f
		}
	}
	if result == nil {
		f.Error = "no diff produced"
		return f
	}

	f.Binary = result.Binary
	f.Large = result.Large
	f.Error = result.Error
	f.Added = result.AddedLines()
	f.Removed = result.DeletedLines()
	if hunks := hunksFrom(result.Lines); len(hunks) > 0 {
		f.Hunks = hunks
	}
	return f
}

// diffAgainst returns one file's diff versus a revision, as the fallback for
// anything the bulk diff could not attribute.
//
// A rename must be asked for by **both** paths. Limited to the new path alone,
// git cannot see the pair and reports the file as newly added -- so a pure
// rename came back as every line added, and a rename with edits came back as a
// whole new file instead of the few lines that actually changed. The bulk diff
// gets this right because it is not path-limited, and this has to agree with it.
func diffAgainst(root, rev string, entry fileEntry) (*git.DiffResult, error) {
	args := []string{"--no-optional-locks", "diff", "--no-color", "--no-ext-diff", "-U3", "--find-renames", rev, "--"}
	if entry.oldPath != "" {
		args = append(args, entry.oldPath)
	}
	args = append(args, entry.path)

	out, err := git.RunGit(root, args...)
	if err != nil {
		return &git.DiffResult{Path: entry.path, Error: err.Error()}, nil
	}
	return git.ParseDiff(entry.path, out), nil
}

// staysInWorkspace reports whether a repo-relative path stays inside the
// workspace.
//
// Paths arrive from the browser, so a comment claiming to be on
// "../../.ssh/config" must not be quoted back to the agent as a file in this
// workspace. Nothing here reads the file, but the agent that receives the
// review will.
//
// The check is lexical, and that is the right scope: the path is never opened,
// only named in a message, so what matters is that it cannot describe somewhere
// outside the repo. It takes no root for exactly that reason -- consulting one
// would imply a containment check against the filesystem that this does not do.
func staysInWorkspace(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	clean := filepath.Clean(rel)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
