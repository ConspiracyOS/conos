package driverutil_test

import (
	"fmt"
	"testing"

	"github.com/ConspiracyOS/conos/internal/driverutil"
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
		got := driverutil.AgentFromPath(tt.path)
		if got != tt.want {
			t.Errorf("AgentFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if driverutil.Truncate("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	if driverutil.Truncate("hello world", 5) != "hello..." {
		t.Errorf("expected 'hello...', got %q", driverutil.Truncate("hello world", 5))
	}
}

func TestResponseTracker(t *testing.T) {
	tracker := driverutil.NewResponseTracker()

	if !tracker.IsNew("a") {
		t.Error("first call should be new")
	}
	if tracker.IsNew("a") {
		t.Error("second call should not be new")
	}
	if tracker.Count() != 1 {
		t.Errorf("expected count 1, got %d", tracker.Count())
	}
}

func TestSeedResponses(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{
			"ls /srv/conos/agents/*/outbox/*.response 2>/dev/null": "/srv/conos/agents/concierge/outbox/001.response\n/srv/conos/agents/sysadmin/outbox/002.response",
		},
	}
	tracker := driverutil.NewResponseTracker()

	driverutil.SeedResponses(mock, tracker)

	if tracker.Count() != 2 {
		t.Errorf("expected 2 seeded responses, got %d", tracker.Count())
	}
	// These should now be marked as seen
	if tracker.IsNew("/srv/conos/agents/concierge/outbox/001.response") {
		t.Error("001.response should have been seeded as seen")
	}
}

func TestSeedResponses_Empty(t *testing.T) {
	mock := &MockExecutor{
		Responses: map[string]string{},
		Errors:    map[string]error{"ls /srv/conos/agents/*/outbox/*.response 2>/dev/null": fmt.Errorf("no matches")},
	}
	tracker := driverutil.NewResponseTracker()

	driverutil.SeedResponses(mock, tracker)

	if tracker.Count() != 0 {
		t.Errorf("expected 0 responses on error, got %d", tracker.Count())
	}
}

func TestDefaultSSHConfig_Defaults(t *testing.T) {
	cfg := driverutil.DefaultSSHConfig("", "", "", "")
	if cfg.Host != "localhost" {
		t.Errorf("expected localhost, got %q", cfg.Host)
	}
	if cfg.Port != "22" {
		t.Errorf("expected 22, got %q", cfg.Port)
	}
	if cfg.User != "root" {
		t.Errorf("expected root, got %q", cfg.User)
	}
	if cfg.Key != "$HOME/.ssh/id_ed25519" {
		t.Errorf("expected default key path, got %q", cfg.Key)
	}
}

func TestDefaultSSHConfig_Override(t *testing.T) {
	cfg := driverutil.DefaultSSHConfig("myhost", "2222", "admin", "/tmp/key")
	if cfg.Host != "myhost" {
		t.Errorf("expected myhost, got %q", cfg.Host)
	}
	if cfg.Port != "2222" {
		t.Errorf("expected 2222, got %q", cfg.Port)
	}
	if cfg.User != "admin" {
		t.Errorf("expected admin, got %q", cfg.User)
	}
	if cfg.Key != "/tmp/key" {
		t.Errorf("expected /tmp/key, got %q", cfg.Key)
	}
}
