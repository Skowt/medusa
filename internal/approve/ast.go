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
	f, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		return nil, err
	}
	var commands []string
	for _, stmt := range f.Stmts {
		commands = extractFromNode(stmt, commands, parser)
	}
	return commands, nil
}

func extractFromNode(node syntax.Node, out []string, parser *syntax.Parser) []string {
	if node == nil {
		return out
	}
	switch n := node.(type) {
	case *syntax.Stmt:
		out = extractFromNode(n.Cmd, out, parser)
		// Check redirections for command/process substitutions
		for _, redir := range n.Redirs {
			out = extractFromWord(redir.Word, out, parser)
			if redir.Hdoc != nil {
				out = extractFromWord(redir.Hdoc, out, parser)
			}
		}

	case *syntax.CallExpr:
		cmdStr := callExprString(n)
		if cmdStr != "" {
			out = append(out, cmdStr)
		}
		// Recurse into command substitutions in arguments
		for _, arg := range n.Args {
			out = extractCmdSubstsFromWord(arg, out, parser)
		}
		// Recurse into assignments
		for _, assign := range n.Assigns {
			if assign.Value != nil {
				out = extractCmdSubstsFromWord(assign.Value, out, parser)
			}
			if assign.Array != nil {
				for _, elem := range assign.Array.Elems {
					if elem.Value != nil {
						out = extractCmdSubstsFromWord(elem.Value, out, parser)
					}
				}
			}
		}
		// Recurse into bash -c / sh -c arguments
		out = maybeRecurseBashC(n, out, parser)

	case *syntax.BinaryCmd:
		out = extractFromNode(n.X, out, parser)
		out = extractFromNode(n.Y, out, parser)

	case *syntax.Subshell:
		for _, stmt := range n.Stmts {
			out = extractFromNode(stmt, out, parser)
		}

	case *syntax.Block:
		for _, stmt := range n.Stmts {
			out = extractFromNode(stmt, out, parser)
		}

	case *syntax.IfClause:
		for _, stmt := range n.Cond {
			out = extractFromNode(stmt, out, parser)
		}
		for _, stmt := range n.Then {
			out = extractFromNode(stmt, out, parser)
		}
		if n.Else != nil {
			out = extractFromNode(n.Else, out, parser)
		}

	case *syntax.WhileClause:
		for _, stmt := range n.Cond {
			out = extractFromNode(stmt, out, parser)
		}
		for _, stmt := range n.Do {
			out = extractFromNode(stmt, out, parser)
		}

	case *syntax.ForClause:
		if wl, ok := n.Loop.(*syntax.WordIter); ok {
			for _, w := range wl.Items {
				out = extractCmdSubstsFromWord(w, out, parser)
			}
		}
		for _, stmt := range n.Do {
			out = extractFromNode(stmt, out, parser)
		}

	case *syntax.CaseClause:
		for _, ci := range n.Items {
			for _, stmt := range ci.Stmts {
				out = extractFromNode(stmt, out, parser)
			}
		}

	case *syntax.DeclClause:
		// export, local, declare, etc.
		for _, assign := range n.Args {
			if assign.Value != nil {
				out = extractCmdSubstsFromWord(assign.Value, out, parser)
			}
			if assign.Array != nil {
				for _, elem := range assign.Array.Elems {
					if elem.Value != nil {
						out = extractCmdSubstsFromWord(elem.Value, out, parser)
					}
				}
			}
		}

	case *syntax.CoprocClause:
		out = extractFromNode(n.Stmt, out, parser)

	case *syntax.ArithmCmd:
		// Arithmetic: no sub-commands

	case *syntax.TestClause:
		// [[ ... ]]: no sub-commands typically

	case *syntax.FuncDecl:
		out = extractFromNode(n.Body, out, parser)
	}
	return out
}

// extractFromWord looks for CmdSubst and ProcSubst in a word's parts.
func extractFromWord(w *syntax.Word, out []string, parser *syntax.Parser) []string {
	if w == nil {
		return out
	}
	return extractCmdSubstsFromWord(w, out, parser)
}

// extractCmdSubstsFromWord recursively finds CmdSubst/ProcSubst in word parts.
func extractCmdSubstsFromWord(w *syntax.Word, out []string, parser *syntax.Parser) []string {
	if w == nil {
		return out
	}
	for _, part := range w.Parts {
		out = extractCmdSubstsFromPart(part, out, parser)
	}
	return out
}

func extractCmdSubstsFromPart(part syntax.WordPart, out []string, parser *syntax.Parser) []string {
	switch p := part.(type) {
	case *syntax.CmdSubst:
		for _, stmt := range p.Stmts {
			out = extractFromNode(stmt, out, parser)
		}
	case *syntax.ProcSubst:
		for _, stmt := range p.Stmts {
			out = extractFromNode(stmt, out, parser)
		}
	case *syntax.DblQuoted:
		for _, inner := range p.Parts {
			out = extractCmdSubstsFromPart(inner, out, parser)
		}
	case *syntax.ParamExp:
		if p.Exp != nil && p.Exp.Word != nil {
			out = extractCmdSubstsFromWord(p.Exp.Word, out, parser)
		}
		if p.Repl != nil {
			if p.Repl.Orig != nil {
				out = extractCmdSubstsFromWord(p.Repl.Orig, out, parser)
			}
			if p.Repl.With != nil {
				out = extractCmdSubstsFromWord(p.Repl.With, out, parser)
			}
		}
	}
	return out
}

// callExprString reconstructs the command string from a CallExpr,
// including any leading environment variable assignments.
func callExprString(ce *syntax.CallExpr) string {
	if len(ce.Args) == 0 && len(ce.Assigns) == 0 {
		return ""
	}
	var buf bytes.Buffer
	printer := syntax.NewPrinter()
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

// maybeRecurseBashC checks if a CallExpr is `bash -c '...'` or `sh -c '...'`
// and recursively extracts commands from the inner script.
func maybeRecurseBashC(ce *syntax.CallExpr, out []string, parser *syntax.Parser) []string {
	if len(ce.Args) < 3 {
		return out
	}
	// Get the base command name
	cmdName := wordToLiteral(ce.Args[0])
	base := cmdName
	// Handle paths like /bin/bash, /usr/bin/env
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}

	// For "env bash -c ..." or "env sh -c ...", skip the "env" arg
	args := ce.Args[1:]
	if base == "env" && len(args) >= 3 {
		cmdName = wordToLiteral(args[0])
		base = cmdName
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
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

	// Get the script argument
	script := wordToLiteral(args[cIdx+1])
	if script == "" {
		return out
	}

	// Recursively parse
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
