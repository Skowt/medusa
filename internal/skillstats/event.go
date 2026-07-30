// Package skillstats tracks which Claude Code skills were invoked across
// Medusa sessions and serves an HTTP dashboard over the result.
//
// The source of truth is Claude Code's own transcript files: every skill
// invocation appears there as a `Skill` tool_use block carrying the skill name,
// so tracking needs no extra hook and works retroactively over sessions that
// already happened. Because Claude Code prunes transcripts (cleanupPeriodDays,
// 30 days by default) while the dashboard reports over months, scanned events
// are copied into a durable append-only log that outlives the transcripts.
package skillstats

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Group buckets for skills whose name carries no `plugin:` prefix. A bare name
// is a plugin-less skill, which is still worth distinguishing by where it comes
// from — the user's own, the project's, or shipped with Claude Code.
const (
	GroupPersonal = "personal"
	GroupProject  = "project"
	GroupBuiltin  = "built-in"
)

// Event is one skill invocation. It is the unit stored in the durable log, one
// JSON object per line.
type Event struct {
	// UUID is the transcript entry's own uuid. It is the dedup key: rescanning
	// a transcript, or a session that forks and replays earlier entries, must
	// not double-count an invocation.
	UUID string `json:"uuid"`
	// TS is when the invocation happened, from the transcript entry.
	TS time.Time `json:"ts"`
	// Profile is the Medusa profile the session ran under, derived from the
	// transcript's location under ~/.medusa/profiles/<profile>/projects.
	Profile string `json:"profile"`
	// Session is Claude Code's session id.
	Session string `json:"session"`
	// Skill is the skill name without its plugin prefix ("explore").
	Skill string `json:"skill"`
	// Plugin is the owning plugin ("cargo-ai-utils"), or one of the Group*
	// buckets when the invocation used a bare skill name.
	Plugin string `json:"plugin"`
	// CWD is the working directory of the session, used to resolve whether a
	// bare skill name belongs to the project.
	CWD string `json:"cwd,omitempty"`
	// Sidechain marks an invocation made by a subagent rather than the main
	// conversation.
	Sidechain bool `json:"sidechain,omitempty"`
}

// Qualified renders the skill the way it was invoked: "plugin:skill" for a
// plugin skill, and the bare name for the Group* buckets, which are
// classifications rather than real prefixes.
func (e Event) Qualified() string {
	switch e.Plugin {
	case GroupPersonal, GroupProject, GroupBuiltin, "":
		return e.Skill
	default:
		return e.Plugin + ":" + e.Skill
	}
}

// resolver classifies bare skill names by looking for the skill on disk. Both
// lookups are filesystem-bound and repeat heavily across a scan, so results are
// memoized. Safe for concurrent use so a scan and an HTTP request can share one.
type resolver struct {
	mu    sync.Mutex
	cache map[string]string
	// userSkills is ~/.claude/skills; empty when the home dir is unavailable.
	userSkills string
}

func newResolver() *resolver {
	r := &resolver{cache: map[string]string{}}
	if home, err := os.UserHomeDir(); err == nil {
		r.userSkills = filepath.Join(home, ".claude", "skills")
	}
	return r
}

// split parses a raw skill name from a Skill tool call into its group and skill
// parts. A "plugin:skill" name groups under the plugin. A bare name is
// classified by locating it on disk, so a personal skill and a Claude Code
// built-in do not land in the same bucket.
func (r *resolver) split(raw, cwd string) (plugin, skill string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if idx := strings.Index(raw, ":"); idx > 0 && idx < len(raw)-1 {
		return raw[:idx], raw[idx+1:]
	}
	return r.classifyBare(raw, cwd), raw
}

// classifyBare buckets a prefix-less skill name. Project skills are keyed by
// cwd as well as name: the same name can be a project skill in one worktree and
// absent in another.
func (r *resolver) classifyBare(name, cwd string) string {
	key := name + "\x00" + cwd
	r.mu.Lock()
	if group, ok := r.cache[key]; ok {
		r.mu.Unlock()
		return group
	}
	r.mu.Unlock()

	group := GroupBuiltin
	switch {
	case r.userSkills != "" && isSkillDir(filepath.Join(r.userSkills, name)):
		group = GroupPersonal
	case cwd != "" && isSkillDir(filepath.Join(cwd, ".claude", "skills", name)):
		group = GroupProject
	}

	r.mu.Lock()
	r.cache[key] = group
	r.mu.Unlock()
	return group
}

// isSkillDir reports whether path is a skill directory. A directory without a
// SKILL.md is not a skill, so a same-named directory cannot misclassify one.
func isSkillDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, "SKILL.md"))
	return err == nil && !info.IsDir()
}
