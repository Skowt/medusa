package shellutil

import "strings"

// Quote wraps a value in single quotes for safe shell embedding.
// Interior single quotes are escaped using the '\” technique.
// Returns ” for empty strings.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
