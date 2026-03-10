package approve

import (
	"bytes"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// IsCompound returns true if the command contains shell metacharacters that
// indicate compound structure (pipes, chains, subshells, substitutions).
func IsCompound(cmd string) bool {
	for _, c := range cmd {
		switch c {
		case '|', '&', ';', '`':
			return true
		}
	}
	return strings.Contains(cmd, "$(") ||
		strings.Contains(cmd, "<(") ||
		strings.Contains(cmd, ">(")
}

// ExtractCommands parses a shell command and returns all individual
// sub-commands found in the AST.
func ExtractCommands(cmd string) ([]string, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	printer := syntax.NewPrinter()
	f, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		return nil, err
	}
	var commands []string
	for _, stmt := range f.Stmts {
		commands = extractFromNode(stmt, commands, printer)
	}
	return commands, nil
}

func extractFromNode(node syntax.Node, out []string, printer *syntax.Printer) []string {
	if node == nil {
		return out
	}
	switch n := node.(type) {
	case *syntax.Stmt:
		out = extractFromNode(n.Cmd, out, printer)
		for _, redir := range n.Redirs {
			out = extractCmdSubstsFromWord(redir.Word, out, printer)
			if redir.Hdoc != nil {
				out = extractCmdSubstsFromWord(redir.Hdoc, out, printer)
			}
		}

	case *syntax.CallExpr:
		cmdStr := callExprString(n, printer)
		if cmdStr != "" {
			out = append(out, cmdStr)
		}
		for _, arg := range n.Args {
			out = extractCmdSubstsFromWord(arg, out, printer)
		}
		out = extractCmdSubstsFromAssigns(n.Assigns, out, printer)
		out = maybeRecurseBashC(n, out, printer)

	case *syntax.BinaryCmd:
		out = extractFromNode(n.X, out, printer)
		out = extractFromNode(n.Y, out, printer)

	case *syntax.Subshell:
		out = extractFromStmts(n.Stmts, out, printer)

	case *syntax.Block:
		out = extractFromStmts(n.Stmts, out, printer)

	case *syntax.IfClause:
		out = extractFromStmts(n.Cond, out, printer)
		out = extractFromStmts(n.Then, out, printer)
		if n.Else != nil {
			out = extractFromNode(n.Else, out, printer)
		}

	case *syntax.WhileClause:
		out = extractFromStmts(n.Cond, out, printer)
		out = extractFromStmts(n.Do, out, printer)

	case *syntax.ForClause:
		if wl, ok := n.Loop.(*syntax.WordIter); ok {
			for _, w := range wl.Items {
				out = extractCmdSubstsFromWord(w, out, printer)
			}
		}
		out = extractFromStmts(n.Do, out, printer)

	case *syntax.CaseClause:
		for _, ci := range n.Items {
			out = extractFromStmts(ci.Stmts, out, printer)
		}

	case *syntax.DeclClause:
		out = extractCmdSubstsFromAssigns(n.Args, out, printer)

	case *syntax.CoprocClause:
		out = extractFromNode(n.Stmt, out, printer)

	case *syntax.ArithmCmd:
		// Arithmetic: no sub-commands

	case *syntax.TestClause:
		// [[ ... ]]: no sub-commands typically

	case *syntax.FuncDecl:
		out = extractFromNode(n.Body, out, printer)
	}
	return out
}

func extractFromStmts(stmts []*syntax.Stmt, out []string, printer *syntax.Printer) []string {
	for _, stmt := range stmts {
		out = extractFromNode(stmt, out, printer)
	}
	return out
}

// extractCmdSubstsFromAssigns extracts command substitutions from a slice
// of assignments (used by both CallExpr and DeclClause).
func extractCmdSubstsFromAssigns(assigns []*syntax.Assign, out []string, printer *syntax.Printer) []string {
	for _, assign := range assigns {
		if assign.Value != nil {
			out = extractCmdSubstsFromWord(assign.Value, out, printer)
		}
		if assign.Array != nil {
			for _, elem := range assign.Array.Elems {
				if elem.Value != nil {
					out = extractCmdSubstsFromWord(elem.Value, out, printer)
				}
			}
		}
	}
	return out
}

// extractCmdSubstsFromWord recursively finds CmdSubst/ProcSubst in word parts.
func extractCmdSubstsFromWord(w *syntax.Word, out []string, printer *syntax.Printer) []string {
	if w == nil {
		return out
	}
	for _, part := range w.Parts {
		out = extractCmdSubstsFromPart(part, out, printer)
	}
	return out
}

func extractCmdSubstsFromPart(part syntax.WordPart, out []string, printer *syntax.Printer) []string {
	switch p := part.(type) {
	case *syntax.CmdSubst:
		out = extractFromStmts(p.Stmts, out, printer)
	case *syntax.ProcSubst:
		out = extractFromStmts(p.Stmts, out, printer)
	case *syntax.DblQuoted:
		for _, inner := range p.Parts {
			out = extractCmdSubstsFromPart(inner, out, printer)
		}
	case *syntax.ParamExp:
		if p.Exp != nil && p.Exp.Word != nil {
			out = extractCmdSubstsFromWord(p.Exp.Word, out, printer)
		}
		if p.Repl != nil {
			if p.Repl.Orig != nil {
				out = extractCmdSubstsFromWord(p.Repl.Orig, out, printer)
			}
			if p.Repl.With != nil {
				out = extractCmdSubstsFromWord(p.Repl.With, out, printer)
			}
		}
	}
	return out
}

// callExprString reconstructs the command string from a CallExpr,
// including any leading environment variable assignments.
func callExprString(ce *syntax.CallExpr, printer *syntax.Printer) string {
	if len(ce.Args) == 0 && len(ce.Assigns) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, assign := range ce.Assigns {
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		_ = printer.Print(&buf, assign)
	}
	for _, arg := range ce.Args {
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		_ = printer.Print(&buf, arg)
	}
	return buf.String()
}

func basename(s string) string {
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// maybeRecurseBashC checks if a CallExpr is `bash -c '...'` or `sh -c '...'`
// and recursively extracts commands from the inner script.
func maybeRecurseBashC(ce *syntax.CallExpr, out []string, printer *syntax.Printer) []string {
	if len(ce.Args) < 3 {
		return out
	}
	base := basename(wordToLiteral(ce.Args[0]))

	// For "env bash -c ..." or "env sh -c ...", skip the "env" arg
	args := ce.Args[1:]
	if base == "env" && len(args) >= 3 {
		base = basename(wordToLiteral(args[0]))
		args = args[1:]
	}

	if base != "bash" && base != "sh" && base != "zsh" {
		return out
	}

	// Find -c flag
	cIdx := -1
	for i, arg := range args {
		if wordToLiteral(arg) == "-c" {
			cIdx = i
			break
		}
	}
	if cIdx < 0 || cIdx+1 >= len(args) {
		return out
	}

	script := wordToLiteral(args[cIdx+1])
	if script == "" {
		return out
	}

	inner, err := ExtractCommands(script)
	if err != nil {
		return out
	}
	return append(out, inner...)
}

// wordToLiteral extracts the literal string value from a word, handling
// simple cases (Lit, SglQuoted, DblQuoted with only Lit parts).
func wordToLiteral(w *syntax.Word) string {
	var buf bytes.Buffer
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			buf.WriteString(p.Value)
		case *syntax.SglQuoted:
			buf.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					buf.WriteString(lit.Value)
				}
			}
		}
	}
	return buf.String()
}
