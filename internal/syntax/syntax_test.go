package syntax

import (
	"strings"
	"testing"
)

func kinds(tokens []Token) string {
	var b strings.Builder
	for _, t := range tokens {
		switch t.Kind {
		case KindKeyword:
			b.WriteByte('K')
		case KindString:
			b.WriteByte('S')
		case KindComment:
			b.WriteByte('C')
		case KindNumber:
			b.WriteByte('N')
		case KindPunct:
			b.WriteByte('P')
		case KindFunction:
			b.WriteByte('F')
		case KindType:
			b.WriteByte('T')
		default:
			b.WriteByte('.')
		}
	}
	return b.String()
}

func lineText(tokens []Token) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.Text)
	}
	return b.String()
}

// TestHighlightPreservesText is the property everything else rests on:
// colouring must never change what is on screen. A lexer whose output drops or
// duplicates a character silently corrupts the code the user is reading, and
// the split-on-newline step is exactly where that would happen.
func TestHighlightPreservesText(t *testing.T) {
	lang, ok := LanguageFor("f.go")
	if !ok {
		t.Fatal("go not recognised")
	}
	lines := []string{
		`func main() { fmt.Println("hi") }`,
		"\tx := 0x1F + 42.5 // a trailing comment",
		`var s = "a \" escaped quote"`,
		`/* block */ return nil`,
		``,
		"\t\t\t",
		`s := ` + "`a raw",
		`string over lines` + "`",
		`}`,
	}

	painted := Highlight(lang, lines)
	if len(painted) != len(lines) {
		t.Fatalf("got %d painted lines for %d input lines", len(painted), len(lines))
	}
	for i, line := range lines {
		if got := lineText(painted[i]); got != line {
			t.Errorf("line %d changed:\n in %q\nout %q", i, line, got)
		}
	}
}

// TestHighlightSeesAcrossLines is the reason a block is lexed rather than a
// line: a raw string spanning lines is invisible to anything fed one line at a
// time, and its contents then colour as if they were code.
func TestHighlightSeesAcrossLines(t *testing.T) {
	lang, _ := LanguageFor("f.go")
	painted := Highlight(lang, []string{
		"s := `open",
		"func not_a_keyword()",
		"still inside`",
	})

	middle := painted[1]
	if len(middle) == 0 {
		t.Fatal("the middle line produced no tokens")
	}
	for _, tok := range middle {
		if tok.Kind == KindKeyword {
			t.Errorf("a keyword was coloured inside a raw string: %q", tok.Text)
		}
		if tok.Kind != KindString {
			t.Errorf("line inside a raw string is %v, want string: %q", tok.Kind, tok.Text)
		}
	}
}

func TestHighlightGo(t *testing.T) {
	lang, _ := LanguageFor("main.go")
	// Adjacent runs of one kind merge, so these are shorter than the token
	// count: the caller styles each stretch once rather than per character.
	cases := []struct{ line, want string }{
		{`// just a comment`, "C"},
		{`x := 42`, ".P.N"},          // "x " | ":=" | " " | 42
		{`s := "hi"`, ".P.S"},        // "s " | ":=" | " " | "hi"
		{`return doIt(x)`, "K.FP.P"}, // return | " " | doIt | "(" | x | ")"
	}
	for _, tc := range cases {
		painted := Highlight(lang, []string{tc.line})
		if got := kinds(painted[0]); got != tc.want {
			t.Errorf("%q: kinds = %s, want %s", tc.line, got, tc.want)
		}
	}
}

// TestUnknownFileIsPlain keeps an unrecognised file from being coloured as if
// it were something else, which reads as corruption rather than as styling —
// and keeps the caller from paying for a lexer to be told it is plain text.
func TestUnknownFileIsPlain(t *testing.T) {
	lang, ok := LanguageFor("notes.unknown-ext")
	if ok {
		t.Fatalf("unexpected language %q for an unknown extension", lang.Name)
	}
	painted := Highlight(lang, []string{`func main() { "x" }`})
	if len(painted[0]) != 1 || painted[0][0].Kind != KindText {
		t.Errorf("unknown file was tokenized: %v", painted[0])
	}
}

// TestLanguageForCommonFiles spot-checks the coverage the swap to chroma bought:
// the hand-rolled tokenizer it replaced knew eleven languages by extension.
func TestLanguageForCommonFiles(t *testing.T) {
	for _, path := range []string{
		"a/b/main.go", "x.tsx", "s.py", "q.sql", "c.yml", "Dockerfile",
		"p.json", "r.rb", "m.rs", "k.kt", "s.swift", "Makefile", "s.scss",
		"v.vue", "t.toml", "c.tf", "p.proto", "a.php", "d.dart", "x.cs",
	} {
		if _, ok := LanguageFor(path); !ok {
			t.Errorf("no lexer for %s", path)
		}
	}
}

// TestFunctionNamesAreDistinguished covers the one structural fact the
// tokenizer looks past a single token for. Without it a line of code is a wall
// of one colour, which is most of what made highlighting worth having.
func TestFunctionNamesAreDistinguished(t *testing.T) {
	lang, _ := LanguageFor("f.go")
	painted := Highlight(lang, []string{"\tresult := compute(value, other)"})

	var funcs, plain []string
	for _, tok := range painted[0] {
		switch tok.Kind {
		case KindFunction:
			funcs = append(funcs, tok.Text)
		case KindText:
			plain = append(plain, tok.Text)
		}
	}
	if len(funcs) != 1 || funcs[0] != "compute" {
		t.Errorf("function names found: %v, want [compute]", funcs)
	}
	if strings.Contains(strings.Join(plain, ""), "compute") {
		t.Error("the called name was also emitted as plain text")
	}
}

// TestBuiltinTypesAreDistinguished covers the second word list.
func TestBuiltinTypesAreDistinguished(t *testing.T) {
	lang, _ := LanguageFor("f.go")
	painted := Highlight(lang, []string{"var count int"})

	got := kinds(painted[0])
	if !strings.Contains(got, "T") {
		t.Errorf("no type token in %q: kinds=%s", "var count int", got)
	}
}
