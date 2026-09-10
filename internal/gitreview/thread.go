package gitreview

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Author names who wrote a comment or reply. The page renders the two
// differently, and the agent is told which of its own messages came back.
const (
	AuthorUser  = "user"
	AuthorAgent = "agent"
)

// Side says whether a comment's line numbers refer to the post-image of the
// file or the pre-image. A comment on a deleted line can only mean the old side.
const (
	SideNew = "new"
	SideOld = "old"
)

// Thread is one review comment and everything said under it.
//
// StartLine and EndLine are real positions in the file, which is what makes a
// thread reportable to an agent: they survive being read minutes later, and a
// range covers the "comment over a group of lines" case without needing a
// separate representation.
type Thread struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Side      string    `json:"side"`
	StartLine int       `json:"startLine"`
	EndLine   int       `json:"endLine"`
	Quote     []string  `json:"quote"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	// Sent records that the agent has been told about this thread. An unsent
	// thread is a draft: it is the user's, and nothing has acted on it yet.
	Sent     bool      `json:"sent"`
	SentAt   time.Time `json:"sentAt,omitempty"`
	Resolved bool      `json:"resolved"`
	Replies  []Reply   `json:"replies"`
}

// Reply is a message added under a thread, by either side.
//
// A user's reply carries the same Sent flag a thread does, and for the same
// reason: it is something the agent has to be told, and a thread that was
// delivered long ago is no longer pending, so without a flag of its own a reply
// written under it would never reach anybody. An agent's own reply is never
// pending -- it came *from* there.
type Reply struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	Sent      bool      `json:"sent"`
	SentAt    time.Time `json:"sentAt,omitempty"`
}

// ReplyOn pairs a pending reply with the thread it belongs to, which is what a
// composed message needs: the reply alone names no file and no line.
type ReplyOn struct {
	Thread Thread
	Reply  Reply
}

// NewComment is what the page posts to open a thread.
type NewComment struct {
	Path      string   `json:"path"`
	Side      string   `json:"side"`
	StartLine int      `json:"startLine"`
	EndLine   int      `json:"endLine"`
	Quote     []string `json:"quote"`
	Body      string   `json:"body"`
}

// normalize repairs a comment posted with a reversed range or an unknown side,
// and reports whether it is usable at all.
//
// A body-less comment is rejected rather than sent: an anchor with nothing said
// about it gives the agent a location and no instruction, which it can only
// guess at.
func (c *NewComment) normalize() bool {
	c.Path = strings.TrimSpace(c.Path)
	c.Body = strings.TrimSpace(c.Body)
	if c.Body == "" || !staysInWorkspace(c.Path) {
		return false
	}
	if c.Side != SideOld {
		c.Side = SideNew
	}
	// Only swap two real line numbers. An omitted EndLine is zero, and swapping
	// that in made a single-line comment start at line 0 -- harmless while the
	// range was only ever printed, and wrong the moment the page began marking
	// every line a comment covers.
	if c.EndLine > 0 && c.EndLine < c.StartLine {
		c.StartLine, c.EndLine = c.EndLine, c.StartLine
	}
	if c.StartLine < 0 {
		c.StartLine = 0
	}
	if c.EndLine < c.StartLine {
		c.EndLine = c.StartLine
	}
	return true
}

// Range renders a thread's anchor the way a person and an agent both read it:
// "path:12" for one line, "path:12-18" for a run.
func (t *Thread) Range() string {
	if t.StartLine <= 0 {
		return t.Path
	}
	if t.EndLine > t.StartLine {
		return t.Path + ":" + strconv.Itoa(t.StartLine) + "-" + strconv.Itoa(t.EndLine)
	}
	return t.Path + ":" + strconv.Itoa(t.StartLine)
}

// newID mints a short random identifier.
//
// Random rather than sequential because thread ids travel to the agent, which
// echoes them back over HTTP: a guessable id lets anything else on the machine
// post a reply as the agent. crypto/rand cannot fail in practice, and a
// time-based fallback keeps a review working rather than erroring if it does.
func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return prefix + hex.EncodeToString(b[:])
}
