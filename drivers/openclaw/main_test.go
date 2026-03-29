package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConspiracyOS/conos/internal/driverutil"
)

func TestParseResponseBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []responseBlock
	}{
		{
			name:  "single block",
			input: "=== concierge: response-001.md ===\nHello world\n",
			expected: []responseBlock{
				{agent: "concierge", key: "concierge:response-001.md", content: "Hello world"},
			},
		},
		{
			name: "multiple blocks",
			input: "=== concierge: response-001.md ===\nFirst response\n" +
				"=== sysadmin: response-002.md ===\nSecond response\n",
			expected: []responseBlock{
				{agent: "concierge", key: "concierge:response-001.md", content: "First response"},
				{agent: "sysadmin", key: "sysadmin:response-002.md", content: "Second response"},
			},
		},
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "block with empty content is filtered",
			input:    "=== concierge: response-001.md ===\n\n",
			expected: nil,
		},
		{
			name:  "block header without colon separator",
			input: "=== concierge ===\nSome content\n",
			expected: []responseBlock{
				{agent: "concierge", key: "concierge:", content: "Some content"},
			},
		},
		{
			name:  "content with multiple lines",
			input: "=== sysadmin: report.md ===\nLine one\nLine two\nLine three\n",
			expected: []responseBlock{
				{agent: "sysadmin", key: "sysadmin:report.md", content: "Line one\nLine two\nLine three"},
			},
		},
		{
			name:  "trailing newlines are trimmed",
			input: "=== concierge: response.md ===\nContent here\n\n\n",
			expected: []responseBlock{
				{agent: "concierge", key: "concierge:response.md", content: "Content here"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResponseBlocks(tt.input)

			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d blocks, got %d", len(tt.expected), len(got))
			}

			for i := range got {
				if got[i].agent != tt.expected[i].agent {
					t.Errorf("block[%d].agent = %q, want %q", i, got[i].agent, tt.expected[i].agent)
				}
				if got[i].key != tt.expected[i].key {
					t.Errorf("block[%d].key = %q, want %q", i, got[i].key, tt.expected[i].key)
				}
				if got[i].content != tt.expected[i].content {
					t.Errorf("block[%d].content = %q, want %q", i, got[i].content, tt.expected[i].content)
				}
			}
		})
	}
}

func TestSendToGateway(t *testing.T) {
	t.Run("successful send", func(t *testing.T) {
		var receivedBody []byte
		var receivedAuth string
		var receivedContentType string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("Authorization")
			receivedContentType = r.Header.Get("Content-Type")
			receivedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		cfg := Config{
			GatewayURL: srv.URL,
			HookToken:  "test-token",
		}
		client := srv.Client()

		err := sendToGateway(client, cfg, "hello from test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedAuth != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", receivedAuth, "Bearer test-token")
		}
		if receivedContentType != "application/json" {
			t.Errorf("Content-Type header = %q, want %q", receivedContentType, "application/json")
		}

		var msg gatewayMessage
		if err := json.Unmarshal(receivedBody, &msg); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}
		if msg.Text != "hello from test" {
			t.Errorf("body text = %q, want %q", msg.Text, "hello from test")
		}
	})

	t.Run("gateway returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		cfg := Config{
			GatewayURL: srv.URL,
			HookToken:  "test-token",
		}
		client := srv.Client()

		err := sendToGateway(client, cfg, "will fail")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got := err.Error(); !contains(got, "500") {
			t.Errorf("error = %q, want it to contain status code 500", got)
		}
	})

	t.Run("gateway returns non-JSON response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway: upstream unavailable"))
		}))
		defer srv.Close()

		cfg := Config{
			GatewayURL: srv.URL,
			HookToken:  "test-token",
		}
		client := srv.Client()

		err := sendToGateway(client, cfg, "will fail")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got := err.Error(); !contains(got, "502") {
			t.Errorf("error = %q, want it to contain status code 502", got)
		}
		if got := err.Error(); !contains(got, "bad gateway") {
			t.Errorf("error = %q, want it to contain response body", got)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- loadConfig tests ---

func TestLoadConfig(t *testing.T) {
	t.Run("all env vars set", func(t *testing.T) {
		t.Setenv("OPENCLAW_HOOK_TOKEN", "my-token")
		t.Setenv("OPENCLAW_GATEWAY_URL", "https://gw.example.com")
		t.Setenv("OPENCLAW_HOOK_PORT", "9999")
		t.Setenv("CONOS_SSH_HOST", "10.0.0.1")
		t.Setenv("CONOS_SSH_PORT", "2222")
		t.Setenv("CONOS_SSH_USER", "deploy")
		t.Setenv("CONOS_SSH_KEY", "/tmp/test-key")

		cfg := loadConfig()

		if cfg.HookToken != "my-token" {
			t.Errorf("HookToken = %q, want %q", cfg.HookToken, "my-token")
		}
		if cfg.GatewayURL != "https://gw.example.com" {
			t.Errorf("GatewayURL = %q, want %q", cfg.GatewayURL, "https://gw.example.com")
		}
		if cfg.HookPort != "9999" {
			t.Errorf("HookPort = %q, want %q", cfg.HookPort, "9999")
		}
		if cfg.SSH.Host != "10.0.0.1" {
			t.Errorf("SSH.Host = %q, want %q", cfg.SSH.Host, "10.0.0.1")
		}
		if cfg.SSH.Port != "2222" {
			t.Errorf("SSH.Port = %q, want %q", cfg.SSH.Port, "2222")
		}
		if cfg.SSH.User != "deploy" {
			t.Errorf("SSH.User = %q, want %q", cfg.SSH.User, "deploy")
		}
		if cfg.SSH.Key != "/tmp/test-key" {
			t.Errorf("SSH.Key = %q, want %q", cfg.SSH.Key, "/tmp/test-key")
		}
	})

	t.Run("defaults applied when optional vars empty", func(t *testing.T) {
		t.Setenv("OPENCLAW_HOOK_TOKEN", "tok")
		t.Setenv("OPENCLAW_GATEWAY_URL", "")
		t.Setenv("OPENCLAW_HOOK_PORT", "")
		t.Setenv("CONOS_SSH_HOST", "")
		t.Setenv("CONOS_SSH_PORT", "")
		t.Setenv("CONOS_SSH_USER", "")
		t.Setenv("CONOS_SSH_KEY", "")

		cfg := loadConfig()

		if cfg.GatewayURL != "http://localhost:18789" {
			t.Errorf("GatewayURL = %q, want default %q", cfg.GatewayURL, "http://localhost:18789")
		}
		if cfg.HookPort != "3847" {
			t.Errorf("HookPort = %q, want default %q", cfg.HookPort, "3847")
		}
		if cfg.SSH.Host != "localhost" {
			t.Errorf("SSH.Host = %q, want default %q", cfg.SSH.Host, "localhost")
		}
		if cfg.SSH.Port != "22" {
			t.Errorf("SSH.Port = %q, want default %q", cfg.SSH.Port, "22")
		}
		if cfg.SSH.User != "root" {
			t.Errorf("SSH.User = %q, want default %q", cfg.SSH.User, "root")
		}
		// SSH.Key default is $HOME/.ssh/id_ed25519, which gets expanded
		if !strings.HasSuffix(cfg.SSH.Key, "/.ssh/id_ed25519") {
			t.Errorf("SSH.Key = %q, want it to end with /.ssh/id_ed25519", cfg.SSH.Key)
		}
	})
}

// --- Webhook handler tests ---

// mockExecutor implements driverutil.Executor for testing.
type mockExecutor struct {
	runFunc func(cmd string) (string, error)
	calls   []string
}

func (m *mockExecutor) Run(cmd string) (string, error) {
	m.calls = append(m.calls, cmd)
	if m.runFunc != nil {
		return m.runFunc(cmd)
	}
	return "", nil
}

// buildWebhookMux recreates the HTTP mux from main() with injected dependencies.
// This mirrors the inline handler logic in main() so we can test it in isolation.
func buildWebhookMux(cfg Config, ssh driverutil.Executor) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token := r.Header.Get("X-Hook-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != cfg.HookToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var req webhookRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if req.Text == "" {
			http.Error(w, "empty message", http.StatusBadRequest)
			return
		}

		cmd := buildTaskCommand(req)

		_, err = ssh.Run(cmd)
		if err != nil {
			http.Error(w, "task delivery failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	return mux
}

func TestWebhookHandler(t *testing.T) {
	cfg := Config{HookToken: "secret-token"}

	t.Run("wrong method returns 405", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/webhook")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
		}
		if len(mock.calls) != 0 {
			t.Errorf("expected no SSH calls, got %d", len(mock.calls))
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		resp, err := http.Post(srv.URL+"/webhook", "application/json", strings.NewReader(`{"text":"hi"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("wrong token returns 401", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhook", strings.NewReader(`{"text":"hi"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hook-Token", "wrong-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("token via query param", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		resp, err := http.Post(
			srv.URL+"/webhook?token=secret-token",
			"application/json",
			strings.NewReader(`{"text":"hello via query"}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if len(mock.calls) != 1 {
			t.Fatalf("expected 1 SSH call, got %d", len(mock.calls))
		}
		want := buildTaskCommand(webhookRequest{Text: "hello via query"})
		if mock.calls[0] != want {
			t.Errorf("SSH command = %q, want %q", mock.calls[0], want)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhook", strings.NewReader(`not json`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hook-Token", "secret-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("empty text returns 400", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhook", strings.NewReader(`{"text":""}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hook-Token", "secret-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("successful task delivery", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhook",
			strings.NewReader(`{"text":"deploy the app","from":"user1","channel":"general","threadId":"thread-123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hook-Token", "secret-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		// Verify response body
		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if result["status"] != "queued" {
			t.Errorf("response status = %q, want %q", result["status"], "queued")
		}

		// Verify SSH command
		if len(mock.calls) != 1 {
			t.Fatalf("expected 1 SSH call, got %d", len(mock.calls))
		}
		want := buildTaskCommand(webhookRequest{
			Text:     "deploy the app",
			From:     "user1",
			Channel:  "general",
			ThreadID: "thread-123",
		})
		if mock.calls[0] != want {
			t.Errorf("SSH command = %q, want %q", mock.calls[0], want)
		}
	})

	t.Run("single quotes in text are escaped", func(t *testing.T) {
		mock := &mockExecutor{}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhook",
			strings.NewReader(`{"text":"it's a test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hook-Token", "secret-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		if len(mock.calls) != 1 {
			t.Fatalf("expected 1 SSH call, got %d", len(mock.calls))
		}
		want := buildTaskCommand(webhookRequest{Text: "it's a test"})
		if mock.calls[0] != want {
			t.Errorf("SSH command = %q, want %q", mock.calls[0], want)
		}
	})

	t.Run("SSH failure returns 500", func(t *testing.T) {
		mock := &mockExecutor{
			runFunc: func(cmd string) (string, error) {
				return "", fmt.Errorf("connection refused")
			},
		}
		mux := buildWebhookMux(cfg, mock)
		srv := httptest.NewServer(mux)
		defer srv.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhook",
			strings.NewReader(`{"text":"will fail"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hook-Token", "secret-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
		}
		body, _ := io.ReadAll(resp.Body)
		if !contains(string(body), "task delivery failed") {
			t.Errorf("body = %q, want it to contain %q", string(body), "task delivery failed")
		}
	})
}

func TestHealthEndpoint(t *testing.T) {
	mock := &mockExecutor{}
	mux := buildWebhookMux(Config{HookToken: "tok"}, mock)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", string(body), "ok")
	}
}
