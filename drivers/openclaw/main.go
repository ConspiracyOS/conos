package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config holds the driver configuration loaded from environment variables.
type Config struct {
	GatewayURL string
	HookToken  string
	HookPort   string
	SSHHost    string
	SSHPort    string
	SSHUser    string
	SSHKey     string
}

func loadConfig() Config {
	cfg := Config{
		GatewayURL: os.Getenv("OPENCLAW_GATEWAY_URL"),
		HookToken:  os.Getenv("OPENCLAW_HOOK_TOKEN"),
		HookPort:   os.Getenv("OPENCLAW_HOOK_PORT"),
		SSHHost:    os.Getenv("CONOS_SSH_HOST"),
		SSHPort:    os.Getenv("CONOS_SSH_PORT"),
		SSHUser:    os.Getenv("CONOS_SSH_USER"),
		SSHKey:     os.Getenv("CONOS_SSH_KEY"),
	}
	if cfg.HookToken == "" {
		log.Fatal("OPENCLAW_HOOK_TOKEN is required")
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://localhost:18789"
	}
	if cfg.HookPort == "" {
		cfg.HookPort = "3847"
	}
	if cfg.SSHHost == "" {
		cfg.SSHHost = "localhost"
	}
	if cfg.SSHPort == "" {
		cfg.SSHPort = "22"
	}
	if cfg.SSHUser == "" {
		cfg.SSHUser = "root"
	}
	if cfg.SSHKey == "" {
		cfg.SSHKey = os.ExpandEnv("$HOME/.ssh/id_ed25519")
	}
	return cfg
}

// Executor abstracts command execution against the conspiracy instance.
type Executor interface {
	Run(cmd string) (string, error)
}

// SSHExecutor runs commands via SSH to the container.
type SSHExecutor struct {
	Config Config
}

func (e *SSHExecutor) Run(cmd string) (string, error) {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-i", e.Config.SSHKey,
		"-p", e.Config.SSHPort,
		fmt.Sprintf("%s@%s", e.Config.SSHUser, e.Config.SSHHost),
		cmd,
	}
	out, err := exec.Command("ssh", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// responseTracker tracks which response files have already been posted.
type responseTracker struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newResponseTracker() *responseTracker {
	return &responseTracker{seen: make(map[string]bool)}
}

func (rt *responseTracker) isNew(path string) bool {
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

func (rt *responseTracker) count() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.seen)
}

// webhookRequest is the JSON body received from the OpenClaw Gateway.
type webhookRequest struct {
	Text     string `json:"text"`
	From     string `json:"from"`
	Channel  string `json:"channel"`
	ThreadID string `json:"threadId"`
}

// gatewayMessage is the JSON body sent to the OpenClaw Gateway.
type gatewayMessage struct {
	Text string `json:"text"`
}

func main() {
	cfg := loadConfig()
	ssh := &SSHExecutor{Config: cfg}
	tracker := newResponseTracker()

	// Seed the tracker with existing responses so we don't replay history
	seedResponses(ssh, tracker)

	mux := http.NewServeMux()

	// Webhook handler: OpenClaw Gateway -> conspiracy inbox
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Authenticate via header or query param
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

		log.Printf("webhook from=%s channel=%s: %s", req.From, req.Channel, truncate(req.Text, 80))

		// Escape single quotes for shell safety
		escaped := strings.ReplaceAll(req.Text, "'", "'\\''")
		cmd := fmt.Sprintf("conctl task '%s'", escaped)

		_, err = ssh.Run(cmd)
		if err != nil {
			log.Printf("task failed: %v", err)
			http.Error(w, "task delivery failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
	})

	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	addr := ":" + cfg.HookPort
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start response poller
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pollResponses(ctx, cfg, ssh, tracker)

	// Start HTTP server
	go func() {
		log.Printf("openclaw driver listening on %s, gateway=%s, ssh=%s@%s:%s",
			addr, cfg.GatewayURL, cfg.SSHUser, cfg.SSHHost, cfg.SSHPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	// Block until signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
}

// --- Response polling ---

// seedResponses marks all existing response files as seen so we don't replay history.
func seedResponses(exec Executor, tracker *responseTracker) {
	out, err := exec.Run("ls /srv/conos/agents/*/outbox/*.response 2>/dev/null")
	if err != nil || out == "" {
		return
	}
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path != "" {
			tracker.isNew(path) // marks as seen
		}
	}
	log.Printf("seeded %d existing responses", tracker.count())
}

// pollResponses checks for new response files and pushes them to the OpenClaw Gateway.
func pollResponses(ctx context.Context, cfg Config, ssh Executor, tracker *responseTracker) {
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}

		out, err := ssh.Run("conctl responses")
		if err != nil {
			log.Printf("poll responses failed: %v", err)
			continue
		}
		if out == "" {
			continue
		}

		// Parse === agent: file === blocks from conctl responses output
		blocks := parseResponseBlocks(out)
		for _, block := range blocks {
			if !tracker.isNew(block.key) {
				continue
			}

			text := fmt.Sprintf("**%s:**\n%s", block.agent, block.content)
			if err := sendToGateway(client, cfg, text); err != nil {
				log.Printf("gateway send failed for %s: %v", block.agent, err)
				continue
			}
			log.Printf("posted response from %s (%d chars)", block.agent, len(block.content))
		}
	}
}

// responseBlock represents a parsed response block from conctl responses output.
type responseBlock struct {
	agent   string
	key     string // agent:file identifier for dedup
	content string
}

// parseResponseBlocks parses the output of `conctl responses` into individual blocks.
// Format: === agent: filename ===\n<content>\n
func parseResponseBlocks(output string) []responseBlock {
	var blocks []responseBlock
	lines := strings.Split(output, "\n")

	var current *responseBlock
	var contentLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "=== ") && strings.HasSuffix(line, " ===") {
			// Save previous block
			if current != nil {
				current.content = strings.TrimSpace(strings.Join(contentLines, "\n"))
				if current.content != "" {
					blocks = append(blocks, *current)
				}
			}

			// Parse header: === agent: file ===
			header := line[4 : len(line)-4]
			parts := strings.SplitN(header, ": ", 2)
			agent := strings.TrimSpace(parts[0])
			file := ""
			if len(parts) > 1 {
				file = strings.TrimSpace(parts[1])
			}

			current = &responseBlock{
				agent: agent,
				key:   agent + ":" + file,
			}
			contentLines = nil
		} else if current != nil {
			contentLines = append(contentLines, line)
		}
	}

	// Save last block
	if current != nil {
		current.content = strings.TrimSpace(strings.Join(contentLines, "\n"))
		if current.content != "" {
			blocks = append(blocks, *current)
		}
	}

	return blocks
}

// sendToGateway posts a message to the OpenClaw Gateway API.
func sendToGateway(client *http.Client, cfg Config, text string) error {
	msg := gatewayMessage{Text: text}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := strings.TrimRight(cfg.GatewayURL, "/") + "/api/v1/send"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.HookToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- Helpers ---

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// agentFromPath extracts agent name from /srv/conos/agents/<name>/outbox/...
func agentFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "agents" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "unknown"
}
