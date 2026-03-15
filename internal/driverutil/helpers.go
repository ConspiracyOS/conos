package driverutil

import "strings"

// AgentFromPath extracts agent name from /srv/conos/agents/<name>/outbox/...
func AgentFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "agents" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "unknown"
}

// Truncate returns s truncated to n characters with "..." appended if needed.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
