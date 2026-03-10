package approve

import (
	"strings"
)

// ParseBashPrefix extracts the command prefix from Claude Code permission
// patterns like:
//
//	"Bash(ls *)"    -> "ls"
//	"Bash(git:*)"   -> "git"
//	"Bash(npm run *)" -> "npm run"
//	"Bash(ls)"      -> "ls"
//
// Returns the prefix and whether it was a valid Bash permission pattern.
func ParseBashPrefix(pattern string) (string, bool) {
	if !strings.HasPrefix(pattern, "Bash(") || !strings.HasSuffix(pattern, ")") {
		return "", false
	}
	inner := pattern[5 : len(pattern)-1] // strip Bash( and )

	// Remove trailing wildcard patterns: " *", "*", ":*"
	inner = strings.TrimSuffix(inner, " *")
	inner = strings.TrimSuffix(inner, ":*")
	inner = strings.TrimSuffix(inner, "*")
	inner = strings.TrimSpace(inner)

	if inner == "" {
		return "", false
	}
	return inner, true
}

// StripEnvAssignments removes leading VAR=val assignments from a command.
// "FOO=bar BAZ=1 ls -la" -> "ls -la"
func StripEnvAssignments(cmd string) string {
	stripped := cmd
	for {
		// Look for pattern: WORD=NONSPACE SPACE ...
		idx := strings.IndexByte(stripped, ' ')
		if idx < 0 {
			break
		}
		word := stripped[:idx]
		eqIdx := strings.IndexByte(word, '=')
		if eqIdx < 1 {
			break
		}
		name := word[:eqIdx]
		if !isValidEnvName(name) {
			break
		}
		stripped = strings.TrimLeft(stripped[idx+1:], " ")
	}
	return stripped
}

func isValidEnvName(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// MatchCommand checks a single command against allow/deny prefix lists.
// Returns "deny", "allow", or "" (unknown).
func MatchCommand(cmd string, perms *Permissions) string {
	candidates := []string{cmd}
	stripped := StripEnvAssignments(cmd)
	if stripped != cmd {
		candidates = append(candidates, stripped)
	}

	// Check deny first (takes precedence)
	for _, c := range candidates {
		for _, prefix := range perms.Deny {
			if matchesPrefix(c, prefix) {
				return "deny"
			}
		}
	}

	// Check allow
	for _, c := range candidates {
		for _, prefix := range perms.Allow {
			if matchesPrefix(c, prefix) {
				return "allow"
			}
		}
	}

	return ""
}

// matchesPrefix returns true if cmd equals prefix, starts with "prefix ",
// or starts with "prefix/".
func matchesPrefix(cmd, prefix string) bool {
	if cmd == prefix {
		return true
	}
	if strings.HasPrefix(cmd, prefix+" ") || strings.HasPrefix(cmd, prefix+"/") {
		return true
	}
	return false
}
