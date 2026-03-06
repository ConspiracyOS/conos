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
		{"/srv/conos/agents/concierge/outbox/001.response", "concierge"},
		{"/srv/conos/agents/sysadmin/outbox/002.response", "sysadmin"},
		{"/srv/conos/agents/researcher/outbox/file.response", "researcher"},
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
			"ls /srv/conos/agents/*/outbox/*.response 2>/dev/null": "/srv/conos/agents/concierge/outbox/001.response\n/srv/conos/agents/sysadmin/outbox/002.response",
		},
	}
	tracker := newResponseTracker()

	seedResponses(mock, tracker)

	if tracker.count() != 2 {
		t.Errorf("expected 2 seeded responses, got %d", tracker.count())
	}
	// These should now be marked as seen
	if tracker.isNew("/srv/conos/agents/concierge/outbox/001.response") {
		t.Error("001.response should have been seeded as seen")
	}
}

func TestSeedResponses_Empty(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{},
		Errors:    map[string]error{"ls /srv/conos/agents/*/outbox/*.response 2>/dev/null": fmt.Errorf("no matches")},
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

func TestRedactSecrets_APIKeyPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"openrouter key",
			"here is my key: sk-or-v1-abcdefghijklmnopqrstuvwxyz12345678",
			"here is my key: [REDACTED]",
		},
		{
			"anthropic key",
			"key=sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ123456",
			"key=[REDACTED]",
		},
		{
			"openai key",
			"OPENAI_KEY=sk-projABCDEFGHIJKLMNOPQRSTUVWXYZ12345678",
			"OPENAI_KEY=[REDACTED]",
		},
		{
			"tailscale key",
			"auth: tskey-auth-abcdefghijk-ABCDEFGHIJK",
			"auth: [REDACTED]",
		},
		{
			"no secrets",
			"just a normal response",
			"just a normal response",
		},
	}
	for _, tt := range tests {
		got := redactSecrets(tt.input, nil)
		if got != tt.want {
			t.Errorf("%s: redactSecrets(%q) = %q, want %q", tt.name, tt.input, got, tt.want)
		}
	}
}

func TestRedactSecrets_LiteralValues(t *testing.T) {
	token := "my-secret-bot-token.xyz.abc"
	input := "token appeared in output: my-secret-bot-token.xyz.abc end"
	got := redactSecrets(input, []string{token})
	if strings.Contains(got, token) {
		t.Errorf("literal secret not redacted: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %q", got)
	}
}

func TestRedactSecrets_EmptyLiterals(t *testing.T) {
	// Empty literal must not panic or corrupt output.
	got := redactSecrets("hello", []string{""})
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestShellEscape(t *testing.T) {
	// Verify the escaping pattern used in the message handler
	input := "it's a test"
	escaped := strings.ReplaceAll(input, "'", "'\\''")
	cmd := fmt.Sprintf("conctl task '%s'", escaped)
	expected := "conctl task 'it'\\''s a test'"
	if cmd != expected {
		t.Errorf("expected %q, got %q", expected, cmd)
	}
}

func TestArtifactPattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ids   []string
	}{
		{
			"artifact colon reference",
			"see artifact:art_0123456789abcdef for details",
			[]string{"art_0123456789abcdef"},
		},
		{
			"artifact path with filename",
			"wrote to /artifacts/art_abcdef0123456789/report.md",
			[]string{"art_abcdef0123456789"},
		},
		{
			"multiple references",
			"artifact:art_0000000000000001 and /artifacts/art_0000000000000002/data.csv",
			[]string{"art_0000000000000001", "art_0000000000000002"},
		},
		{
			"no match",
			"just a normal response with no artifacts",
			nil,
		},
	}
	for _, tt := range tests {
		matches := artifactPattern.FindAllStringSubmatch(tt.input, -1)
		var ids []string
		for _, m := range matches {
			ids = append(ids, m[1])
		}
		if len(ids) != len(tt.ids) {
			t.Errorf("%s: expected %d matches, got %d", tt.name, len(tt.ids), len(ids))
			continue
		}
		for i, id := range ids {
			if id != tt.ids[i] {
				t.Errorf("%s: match[%d] = %q, want %q", tt.name, i, id, tt.ids[i])
			}
		}
	}
}

func TestResolveArtifactLinks_Success(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{
			"conctl artifact link --base-url 'https://example.com' art_0123456789abcdef": `{"url":"https://example.com/a/art_0123456789abcdef?sig=abc","expires_at":"2026-03-07T00:00:00Z"}`,
		},
	}
	input := "check artifact:art_0123456789abcdef for the report"
	got := resolveArtifactLinks(input, "https://example.com", mock)
	expected := "check https://example.com/a/art_0123456789abcdef?sig=abc for the report"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestResolveArtifactLinks_PathWithFilename(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{
			"conctl artifact link --base-url 'https://example.com' art_abcdef0123456789": `{"url":"https://example.com/a/art_abcdef0123456789?sig=xyz","expires_at":"2026-03-07T00:00:00Z"}`,
		},
	}
	input := "wrote to /artifacts/art_abcdef0123456789/report.md"
	got := resolveArtifactLinks(input, "https://example.com", mock)
	expected := "wrote to https://example.com/a/art_abcdef0123456789?sig=xyz"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestResolveArtifactLinks_Failure(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{},
		Errors: map[string]error{
			"conctl artifact link --base-url 'https://example.com' art_0000000000000000": fmt.Errorf("not found"),
		},
	}
	input := "check artifact:art_0000000000000000 please"
	got := resolveArtifactLinks(input, "https://example.com", mock)
	if got != input {
		t.Errorf("expected original text on failure, got %q", got)
	}
}

func TestResolveArtifactLinks_NoMatches(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{},
	}
	input := "no artifacts here"
	got := resolveArtifactLinks(input, "https://example.com", mock)
	if got != input {
		t.Errorf("expected unchanged text, got %q", got)
	}
}

func TestResolveArtifactLinks_DuplicateIDs(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{
			"conctl artifact link --base-url 'https://example.com' art_1111111111111111": `{"url":"https://example.com/a/art_1111111111111111?sig=s","expires_at":"2026-03-07T00:00:00Z"}`,
		},
	}
	input := "artifact:art_1111111111111111 and again artifact:art_1111111111111111"
	got := resolveArtifactLinks(input, "https://example.com", mock)
	expected := "https://example.com/a/art_1111111111111111?sig=s and again https://example.com/a/art_1111111111111111?sig=s"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
	// Should only call SSH once for the duplicate ID
	callCount := 0
	for _, c := range mock.Calls {
		if strings.Contains(c, "artifact link") {
			callCount++
		}
	}
	if callCount != 1 {
		t.Errorf("expected 1 SSH call for duplicate ID, got %d", callCount)
	}
}

func TestResolveArtifactLinks_InvalidJSON(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{
			"conctl artifact link --base-url 'https://example.com' art_2222222222222222": `not json`,
		},
	}
	input := "artifact:art_2222222222222222"
	got := resolveArtifactLinks(input, "https://example.com", mock)
	if got != input {
		t.Errorf("expected original text on invalid JSON, got %q", got)
	}
}
