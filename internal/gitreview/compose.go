package gitreview

import (
	"strconv"
	"strings"
)

// Compose builds the message pasted into the agent when a review is submitted.
//
// Three things about the shape are deliberate:
//
//  1. Each note names path:line and quotes the lines it hangs off. The quote is
//     what makes the note survive the agent reading it a minute later, by which
//     time the line number may have moved: the agent can find the place by text
//     when the number no longer lands on it.
//  2. The reply instructions come first, before any note. An agent that reads
//     the notes and starts working has already decided what to do by the time it
//     reaches the tail of a long review, and will not go back for a protocol it
//     did not know it needed.
//  3. Every note carries its own thread id. Without one the agent can reply but
//     cannot say which comment it is replying to, and the page has nowhere to
//     put the answer.
//
// It is kept short on purpose. Everything here is read before the agent looks at
// a single comment, so every line of protocol is a line of the actual review it
// is pushed further away from. Closing a thread is not mentioned at all: the
// user resolves their own comments, and an agent told it may resolve them closes
// things it has not really dealt with.
func Compose(replyURL string, agent AgentProfile, threads []Thread) string {
	if len(threads) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Code review from the Medusa review page — ")
	b.WriteString(plural(len(threads), "comment", "comments"))
	b.WriteString(" on your changes. Please action ")
	if len(threads) == 1 {
		b.WriteString("it.\n\n")
	} else {
		b.WriteString("them.\n\n")
	}

	if !agent.CanReply {
		// Saying nothing at all would be worse: the agent would have no idea a
		// reply was ever expected of it. Saying "run this curl" would be worse
		// still -- a blocked connection surfaces as an ordinary "couldn't
		// connect", so the agent would report the review page as broken and the
		// user would go looking for a server that is running perfectly well.
		b.WriteString("You cannot reply to these comments from this session, so do not try:\n")
		b.WriteString("this sandbox has no network access and the review page is unreachable\n")
		b.WriteString("from it. Just make the changes. The page watches the diff and shows\n")
		b.WriteString("the user your work as you go; they have been told replies are off.\n")
		b.WriteString(composeNotes(threads, true))
		return b.String()
	}

	b.WriteString("To answer a comment, POST its thread id and your reply:\n\n")
	b.WriteString("  curl -sS -X POST " + shellQuote(replyURL) + " \\\n")
	b.WriteString("    -H 'Content-Type: application/json' \\\n")
	b.WriteString("    -d '{\"thread\":\"<thread-id>\",\"body\":\"<your reply>\"}'\n\n")
	b.WriteString("Each comment below gives its thread id. Replies appear under the comment\n")
	b.WriteString("on the review page. Write them in simple, direct English (ASD-STE100\n")
	b.WriteString("style): one or two clear sentences, three at the very most, and a short\n")
	b.WriteString("paragraph only when the answer truly needs one.\n")

	b.WriteString(composeNotes(threads, true))
	return b.String()
}

// ComposeFollowUp builds the message for a comment written after the agent has
// already been given the reply instructions.
//
// It is short on purpose. The agent is mid-task and already knows how to answer,
// so repeating the curl, the reply style and the framing costs it context and
// tells it nothing: what it needs is the file, the lines, the comment, and the
// thread id to reply against. Anything it has forgotten it can ask about; the
// protocol comes back in full anyway the moment the review is aimed at a
// different agent (Session.needsProtocol).
//
// Separators are only drawn between several comments. On a single one -- the
// common case, since this is the message auto-send produces per comment -- a
// horizontal rule above three lines of text is noise.
func ComposeFollowUp(threads []Thread) string {
	if len(threads) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("New review ")
	if len(threads) == 1 {
		b.WriteString("comment")
	} else {
		b.WriteString("comments")
	}
	b.WriteString(" from the Medusa review page:")
	b.WriteString(composeNotes(threads, len(threads) > 1))
	return b.String()
}

// composeNotes renders the comments themselves.
//
// The thread id rides along even when the agent cannot reply: it costs one short
// line, and it is what lets the user quote an id back at the agent in the
// terminal when the page's own channel is unavailable.
func composeNotes(threads []Thread, separate bool) string {
	var b strings.Builder
	for _, t := range threads {
		if separate {
			b.WriteString("\n---\n")
		} else {
			b.WriteString("\n")
		}
		b.WriteString(t.Range())
		if t.Side == SideOld {
			b.WriteString(" (on the removed line")
			if t.EndLine > t.StartLine {
				b.WriteString("s")
			}
			b.WriteString(")")
		}
		b.WriteString("  [thread-id: " + t.ID + "]\n")
		for _, q := range t.Quote {
			b.WriteString("> " + q + "\n")
		}
		b.WriteString(t.Body + "\n")
		// A reply the reader added before submitting is part of the comment, not
		// a separate event: the agent is seeing all of it for the first time.
		for _, r := range t.Replies {
			if r.Author == AuthorUser && !r.Sent {
				b.WriteString("also: " + r.Body + "\n")
			}
		}
	}
	return b.String()
}

// ComposeReplies builds the message for replies on comments the agent already
// has.
//
// Each one names its thread twice over -- by id, and by the file and lines it
// hangs off -- because a reply on its own names no place at all, and an agent
// several turns down the task may no longer have the original in front of it.
// The comment being replied to is quoted for the same reason: without it the
// reply is an answer to a question the agent cannot see.
func ComposeReplies(replies []ReplyOn) string {
	if len(replies) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("New ")
	if len(replies) == 1 {
		b.WriteString("reply")
	} else {
		b.WriteString("replies")
	}
	b.WriteString(" from the Medusa review page:")

	for _, r := range replies {
		if len(replies) > 1 {
			b.WriteString("\n---\n")
		} else {
			b.WriteString("\n")
		}
		b.WriteString(r.Thread.Range())
		b.WriteString("  [thread-id: " + r.Thread.ID + "]\n")
		b.WriteString("on your earlier comment: " + oneLine(r.Thread.Body) + "\n")
		b.WriteString(r.Reply.Body + "\n")
	}
	return b.String()
}

// oneLine flattens a body onto a single line for the "replying to" recap. The
// original comment is context here, not the instruction, and a long one repeated
// in full would bury the reply that is the point of the message.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len(flat) > 120 {
		return flat[:119] + "\u2026"
	}
	return flat
}

// plural renders a count with its noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// shellQuote wraps a string for the single-quoted shell context the composed
// command puts it in.
//
// The URL carries a random session token, so it is not attacker-controlled, but
// it is interpolated into a command an agent will run: quoting it is the
// difference between a URL and an instruction.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
