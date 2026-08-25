package review

import (
	"strconv"
	"strings"
)

// composeReview builds the message pasted into the agent.
//
// Each note names file:line and quotes the source it hangs off, so the agent
// can find the place without guessing and without the line number having to
// still be accurate by the time it reads it. Hand edits are listed last with an
// explicit instruction to re-read: the agent has those files in context from
// before the edit, and will otherwise keep reasoning about its own version.
func (m *Model) composeReview(edited []string) string {
	var b strings.Builder
	b.WriteString("I reviewed your changes:\n")

	for _, f := range m.files {
		notes := m.comments[f.Path()]
		if len(notes) == 0 {
			continue
		}
		for _, note := range notes {
			b.WriteString("\n")
			b.WriteString(f.Path())
			if note.Line > 0 {
				b.WriteString(":" + strconv.Itoa(note.Line))
			}
			if note.Stale {
				// Say the anchor moved rather than quoting a line the agent
				// can no longer find. Without this the agent goes looking for
				// text that is not there and reports the comment as bogus.
				b.WriteString(" (you have since changed this line)")
			}
			b.WriteString("\n")
			if quote := trimBlank(note.Quote); quote != "" {
				b.WriteString("> " + quote + "\n")
			}
			b.WriteString(note.Body + "\n")
		}
	}

	if len(edited) > 0 {
		b.WriteString("\nI also edited ")
		b.WriteString(plural(len(edited), "file", "files"))
		b.WriteString(" by hand — re-read them before continuing:\n")
		for _, path := range edited {
			b.WriteString("- " + path + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
