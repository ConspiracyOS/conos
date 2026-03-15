package driverutil

import (
	"log"
	"strings"
	"sync"
)

// ResponseTracker tracks which response files have already been posted,
// preventing duplicate delivery across poll cycles.
type ResponseTracker struct {
	mu   sync.Mutex
	seen map[string]bool
}

// NewResponseTracker returns a new empty tracker.
func NewResponseTracker() *ResponseTracker {
	return &ResponseTracker{seen: make(map[string]bool)}
}

// IsNew returns true if path has not been seen before, and marks it as seen.
// Automatically resets the seen set when it exceeds 10,000 entries.
func (rt *ResponseTracker) IsNew(path string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.seen[path] {
		return false
	}
	rt.seen[path] = true
	if len(rt.seen) > 10000 {
		rt.seen = make(map[string]bool)
	}
	return true
}

// Count returns the number of tracked response paths.
func (rt *ResponseTracker) Count() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.seen)
}

// SeedResponses marks all existing response files as seen so drivers
// don't replay history on startup.
func SeedResponses(exec Executor, tracker *ResponseTracker) {
	out, err := exec.Run("ls /srv/conos/agents/*/outbox/*.response 2>/dev/null")
	if err != nil || out == "" {
		return
	}
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path != "" {
			tracker.IsNew(path) // marks as seen
		}
	}
	log.Printf("seeded %d existing responses", tracker.Count())
}
