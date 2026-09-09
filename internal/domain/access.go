package domain

import "strings"

// ParseUsers extracts clean usernames from ops.txt or whitelist.txt.
func ParseUsers(content string) []string {
	var out []string
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// Fields splits a whitespace-separated list of IDs.
func Fields(s string) []string {
	return strings.Fields(s)
}
