// Package syntax turns source text into coloured runs for the TUI.
//
// It is deliberately a shape-based tokenizer rather than a set of real
// grammars. chroma was tried here and does the job properly, but its lexers add
// 4.7MB to the binary and take ~30ms on a few-hundred-line file — thirty times
// this — which is felt as scroll lag in a view that redraws per keystroke. What
// a terminal palette can show is six or seven colours, and getting those right
// does not need a parser.
//
// It returns kinds, never colours: the mapping to a palette belongs to whichever
// view is drawing, and this package stays free of UI imports so it can be tested
// on strings alone.
package syntax

import (
	"path/filepath"
	"strings"
)

// Kind classifies a token for colouring.
type Kind int

const (
	KindText     Kind = iota // identifiers, whitespace, anything unclaimed
	KindKeyword              // language keywords
	KindString               // quoted strings and runes
	KindComment              // line and block comments
	KindNumber               // numeric literals
	KindPunct                // brackets, operators, separators
	KindFunction             // a name being called or declared
	KindType                 // built-in and declared type names
)

// Token is a run of source text sharing one kind.
type Token struct {
	Kind Kind
	Text string
}

// Language describes how one family of files is tokenized. Languages sharing a
// comment and quoting style share a definition; only the word lists differ.
type Language struct {
	Name string
	// LineComment can hold more than one marker: SQL takes `--`, and several
	// languages accept `#` alongside their primary form.
	LineComment []string
	BlockOpen   string
	BlockClose  string
	// Quotes are the characters that open a string, closed by the same
	// character — true of every language handled here.
	Quotes string
	// RawQuote spans lines when set (Go's backtick, JavaScript's template
	// literal), which is why Highlight has to carry state between them.
	RawQuote byte
	Keywords map[string]bool
	Types    map[string]bool
}

// state is what one line of tokenizing leaves behind for the next.
//
// Without it a block comment or a multi-line string is invisible: fed a line at
// a time, the lexer restarts clean on each and colours the body of a comment as
// though it were code.
type state struct {
	inBlock bool
	inRaw   bool
}

// LanguageFor returns the language for a path, and whether one was recognised.
// An unrecognised file tokenizes as plain text rather than guessing: colouring
// a Makefile as if it were Go is worse than leaving it alone.
func LanguageFor(path string) (Language, bool) {
	name, ok := byExtension[strings.ToLower(filepath.Ext(path))]
	if !ok {
		// A few files carry their type in the name rather than an extension.
		switch strings.ToLower(filepath.Base(path)) {
		case "dockerfile", "makefile", ".bashrc", ".zshrc", ".profile":
			return languages["shell"], true
		}
		return Language{}, false
	}
	return languages[name], true
}

// Highlight lexes a block of source and returns its tokens split by line, one
// entry per line of the input.
//
// It takes a block rather than a line so a comment or string spanning lines is
// seen. Callers pass the slice they are about to draw; a window opening inside
// such a construct still lexes from the wrong state, which is bounded and local.
func Highlight(lang Language, lines []string) [][]Token {
	out := make([][]Token, len(lines))
	var st state
	for i, line := range lines {
		out[i], st = tokenizeLine(lang, line, st)
	}
	return out
}

// tokenizeLine splits one line, resuming from the previous line's state and
// returning the state the next one should resume from.
func tokenizeLine(lang Language, line string, st state) ([]Token, state) {
	if lang.Name == "" {
		return []Token{{Kind: KindText, Text: line}}, st
	}

	var out []Token
	emit := func(kind Kind, text string) {
		if text == "" {
			return
		}
		// Merge with the previous run of the same kind so the caller styles
		// each stretch once instead of per character.
		if n := len(out); n > 0 && out[n-1].Kind == kind {
			out[n-1].Text += text
			return
		}
		out = append(out, Token{Kind: kind, Text: text})
	}

	runes := []rune(line)
	i := 0

	// A construct left open by the previous line runs until it closes, or to
	// the end of this one.
	switch {
	case st.inBlock:
		if end := strings.Index(line, lang.BlockClose); end >= 0 {
			stop := end + len(lang.BlockClose)
			emit(KindComment, line[:stop])
			i = len([]rune(line[:stop]))
			st.inBlock = false
		} else {
			return []Token{{Kind: KindComment, Text: line}}, st
		}
	case st.inRaw:
		if end := strings.IndexByte(line, lang.RawQuote); end >= 0 {
			emit(KindString, line[:end+1])
			i = len([]rune(line[:end+1]))
			st.inRaw = false
		} else {
			return []Token{{Kind: KindString, Text: line}}, st
		}
	}

	for i < len(runes) {
		rest := string(runes[i:])

		if _, ok := matchesAny(rest, lang.LineComment); ok {
			emit(KindComment, rest)
			break
		}
		if lang.BlockOpen != "" && strings.HasPrefix(rest, lang.BlockOpen) {
			end := strings.Index(rest[len(lang.BlockOpen):], lang.BlockClose)
			if end < 0 {
				emit(KindComment, rest)
				st.inBlock = true
				break
			}
			stop := len(lang.BlockOpen) + end + len(lang.BlockClose)
			emit(KindComment, rest[:stop])
			i += len([]rune(rest[:stop]))
			continue
		}
		if lang.RawQuote != 0 && runes[i] == rune(lang.RawQuote) {
			if end := strings.IndexByte(rest[1:], lang.RawQuote); end >= 0 {
				emit(KindString, rest[:end+2])
				i += len([]rune(rest[:end+2]))
				continue
			}
			emit(KindString, rest)
			st.inRaw = true
			break
		}
		if strings.ContainsRune(lang.Quotes, runes[i]) {
			text, width := scanString(runes[i:])
			emit(KindString, text)
			i += width
			continue
		}
		if isDigit(runes[i]) && startsToken(runes, i) {
			text, width := scanNumber(runes[i:])
			emit(KindNumber, text)
			i += width
			continue
		}
		if isWordStart(runes[i]) {
			text, width := scanWord(runes[i:])
			emit(wordKind(lang, text, runes, i+width), text)
			i += width
			continue
		}
		if isPunct(runes[i]) {
			emit(KindPunct, string(runes[i]))
			i++
			continue
		}
		emit(KindText, string(runes[i]))
		i++
	}
	return out, st
}

// wordKind classifies an identifier by what it is and what follows it.
//
// A name immediately followed by "(" is being called or declared, which is the
// one structural fact worth a colour of its own and costs a single lookahead —
// it is what separates a wall of same-coloured identifiers into something
// readable.
func wordKind(lang Language, word string, runes []rune, after int) Kind {
	switch {
	case lang.Keywords[word]:
		return KindKeyword
	case lang.Types[word]:
		return KindType
	}
	for i := after; i < len(runes); i++ {
		if runes[i] == ' ' || runes[i] == '\t' {
			continue
		}
		if runes[i] == '(' {
			return KindFunction
		}
		break
	}
	return KindText
}

func matchesAny(s string, prefixes []string) (string, bool) {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return p, true
		}
	}
	return "", false
}

// scanString consumes a quoted run, honouring backslash escapes. An unclosed
// quote takes the rest of the line, which is what an editor mid-edit looks like.
func scanString(runes []rune) (string, int) {
	quote := runes[0]
	for i := 1; i < len(runes); i++ {
		if runes[i] == '\\' {
			i++
			continue
		}
		if runes[i] == quote {
			return string(runes[:i+1]), i + 1
		}
	}
	return string(runes), len(runes)
}

func scanNumber(runes []rune) (string, int) {
	i := 0
	for i < len(runes) && (isDigit(runes[i]) || runes[i] == '.' || runes[i] == 'x' ||
		runes[i] == 'b' || runes[i] == 'o' || runes[i] == '_' ||
		(runes[i] >= 'a' && runes[i] <= 'f') || (runes[i] >= 'A' && runes[i] <= 'F')) {
		i++
	}
	return string(runes[:i]), i
}

func scanWord(runes []rune) (string, int) {
	i := 0
	for i < len(runes) && isWordPart(runes[i]) {
		i++
	}
	return string(runes[:i]), i
}

// startsToken reports whether index i begins a token rather than continuing an
// identifier, so the "8" in "utf8" is not coloured as a number.
func startsToken(runes []rune, i int) bool {
	return i == 0 || !isWordPart(runes[i-1])
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isWordStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isWordPart(r rune) bool { return isWordStart(r) || isDigit(r) }

func isPunct(r rune) bool {
	return strings.ContainsRune("{}[]()<>.,;:!?=+-*/%&|^~@#$", r)
}
