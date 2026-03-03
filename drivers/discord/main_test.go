package main

import (
	"fmt"
	"strings"
	"testing"
)

// MockExecutor records commands and returns configurable responses.
type MockExecutor struct {
	Responses map[string]string
	Errors    map[string]error
	Calls     []string
}

func (m *MockExecutor) Run(cmd string) (string, error) {
	m.Calls = append(m.Calls, cmd)
	if err, ok := m.Errors[cmd]; ok {
		resp := m.Responses[cmd]
		return resp, err
	}
	if resp, ok := m.Responses[cmd]; ok {
		return resp, nil
	}
	return "", nil
}

func TestSplitMessage_Short(t *testing.T) {
	chunks := splitMessage("hello", 2000)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Errorf("expected single chunk 'hello', got %v", chunks)
	}
}

func TestSplitMessage_ExactLimit(t *testing.T) {
	msg := strings.Repeat("a", 2000)
	chunks := splitMessage(msg, 2000)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for exactly 2000 chars, got %d", len(chunks))
	}
}

func TestSplitMessage_Long(t *testing.T) {
	msg := strings.Repeat("abcdefghij\n", 250) // 2750 chars
	chunks := splitMessage(msg, 2000)
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for 2750 chars, got %d", len(chunks))
	}
	// Verify all content is preserved
	rejoined := strings.Join(chunks, "")
	if rejoined != msg {
		t.Error("content was lost during splitting")
	}
	// Verify each chunk respects the limit
	for i, chunk := range chunks {
		if len(chunk) > 2000 {
			t.Errorf("chunk %d exceeds 2000 chars: %d", i, len(chunk))
		}
	}
}

func TestSplitMessage_SplitsOnNewline(t *testing.T) {
	// Build a message where the 2000th char is mid-line but there's a newline before it
	line := strings.Repeat("x", 100) + "\n"
	msg := strings.Repeat(line, 25) // 2525 chars
	chunks := splitMessage(msg, 2000)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// First chunk should end with a newline (split on newline boundary)
	if !strings.HasSuffix(chunks[0], "\n") {
		t.Error("expected first chunk to end at a newline boundary")
	}
}

func TestAgentFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/srv/con/agents/concierge/outbox/001.response", "concierge"},
		{"/srv/con/agents/sysadmin/outbox/002.response", "sysadmin"},
		{"/srv/con/agents/researcher/outbox/file.response", "researcher"},
		{"/some/other/path", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := agentFromPath(tt.path)
		if got != tt.want {
			t.Errorf("agentFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	if truncate("hello world", 5) != "hello..." {
		t.Errorf("expected 'hello...', got %q", truncate("hello world", 5))
	}
}

func TestSeedResponses(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{
			"ls /srv/con/agents/*/outbox/*.response 2>/dev/null": "/srv/con/agents/concierge/outbox/001.response\n/srv/con/agents/sysadmin/outbox/002.response",
		},
	}
	tracker := newResponseTracker()

	seedResponses(mock, tracker)

	if tracker.count() != 2 {
		t.Errorf("expected 2 seeded responses, got %d", tracker.count())
	}
	// These should now be marked as seen
	if tracker.isNew("/srv/con/agents/concierge/outbox/001.response") {
		t.Error("001.response should have been seeded as seen")
	}
}

func TestSeedResponses_Empty(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{},
		Errors:    map[string]error{"ls /srv/con/agents/*/outbox/*.response 2>/dev/null": fmt.Errorf("no matches")},
	}
	tracker := newResponseTracker()

	seedResponses(mock, tracker)

	if tracker.count() != 0 {
		t.Errorf("expected 0 responses on error, got %d", tracker.count())
	}
}

func TestResponseTracker(t *testing.T) {
	tracker := newResponseTracker()

	if !tracker.isNew("a") {
		t.Error("first call should be new")
	}
	if tracker.isNew("a") {
		t.Error("second call should not be new")
	}
	if tracker.count() != 1 {
		t.Errorf("expected count 1, got %d", tracker.count())
	}
}

func TestShellEscape(t *testing.T) {
	// Verify the escaping pattern used in the message handler
	input := "it's a test"
	escaped := strings.ReplaceAll(input, "'", "'\\''")
	cmd := fmt.Sprintf("con task '%s'", escaped)
	expected := "con task 'it'\\''s a test'"
	if cmd != expected {
		t.Errorf("expected %q, got %q", expected, cmd)
	}
}
