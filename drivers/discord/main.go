package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ConspiracyOS/conos/internal/driverutil"
	"github.com/bwmarrin/discordgo"
)

// typing tracks channels where we're showing the typing indicator.
var typing sync.Map // channelID → context.CancelFunc

// startTyping begins showing "typing..." on a channel. Repeats every 8s until stopTyping is called.
func startTyping(s *discordgo.Session, channelID string) {
	// Cancel any existing typing on this channel
	if prev, ok := typing.LoadAndDelete(channelID); ok {
		prev.(context.CancelFunc)()
	}
	ctx, cancel := context.WithCancel(context.Background())
	typing.Store(channelID, cancel)
	go func() {
		for {
			s.ChannelTyping(channelID)
			select {
			case <-ctx.Done():
				return
			case <-time.After(8 * time.Second):
			}
		}
	}()
}

// stopTyping cancels the typing indicator on a channel.
func stopTyping(channelID string) {
	if cancel, ok := typing.LoadAndDelete(channelID); ok {
		cancel.(context.CancelFunc)()
	}
}

var startTime = time.Now()

// Config holds the driver configuration loaded from environment variables.
type Config struct {
	BotToken  string
	ChannelID string // empty = DM mode
	SSH       driverutil.SSHConfig
	BaseURL   string // for artifact link minting
}

// envWithFallback reads the new CONOS_SSH_* var first, falling back to the old CON_SSH_* name.
func envWithFallback(newKey, oldKey string) string {
	if v := os.Getenv(newKey); v != "" {
		return v
	}
	return os.Getenv(oldKey)
}

func loadConfig() Config {
	cfg := Config{
		BotToken:  os.Getenv("DISCORD_BOT_TOKEN"),
		ChannelID: os.Getenv("DISCORD_CHANNEL_ID"),
		SSH: driverutil.DefaultSSHConfig(
			envWithFallback("CONOS_SSH_HOST", "CON_SSH_HOST"),
			envWithFallback("CONOS_SSH_PORT", "CON_SSH_PORT"),
			envWithFallback("CONOS_SSH_USER", "CON_SSH_USER"),
			envWithFallback("CONOS_SSH_KEY", "CON_SSH_KEY"),
		),
		BaseURL: os.Getenv("CONOS_BASE_URL"),
	}
	if cfg.BotToken == "" {
		log.Fatal("DISCORD_BOT_TOKEN is required")
	}
	if cfg.SSH.Key == "$HOME/.ssh/id_ed25519" {
		cfg.SSH.Key = os.ExpandEnv(cfg.SSH.Key)
	}
	if strings.ContainsRune(cfg.BaseURL, '\'') {
		log.Fatalf("CONOS_BASE_URL contains invalid character (single quote)")
	}
	return cfg
}

// dmChannels tracks active DM channel IDs for response delivery.
type dmChannels struct {
	mu       sync.Mutex
	channels map[string]bool // channelID -> true
}

func newDMChannels() *dmChannels {
	return &dmChannels{channels: make(map[string]bool)}
}

func (d *dmChannels) add(channelID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels[channelID] = true
}

func (d *dmChannels) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.channels))
	for id := range d.channels {
		out = append(out, id)
	}
	return out
}

func (d *dmChannels) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.channels)
}

// Slash command definitions
var slashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "status",
		Description: "Show agent status",
	},
	{
		Name:        "clear",
		Description: "Reset the concierge conversation history",
	},
	{
		Name:        "logs",
		Description: "Show recent audit log entries",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "count",
				Description: "Number of lines (default: 20)",
				Required:    false,
			},
		},
	},
	{
		Name:        "responses",
		Description: "Show latest response from each agent",
	},
	{
		Name:        "history",
		Description: "Show recent agent responses chronologically",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "count",
				Description: "Number of responses (default: 5)",
				Required:    false,
			},
		},
	},
	{
		Name:        "debug",
		Description: "Show driver diagnostics",
	},
}

func main() {
	cfg := loadConfig()

	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		log.Fatalf("creating Discord session: %v", err)
	}

	// Intents: DM messages (no privileged intent needed) + guild messages if channel mode
	dg.Identify.Intents = discordgo.IntentsDirectMessages
	if cfg.ChannelID != "" {
		dg.Identify.Intents |= discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent
	}

	exec := &driverutil.SSHExecutor{Config: cfg.SSH}
	tracker := driverutil.NewResponseTracker()
	dms := newDMChannels()

	// Seed the tracker with existing responses so we only post new ones
	driverutil.SeedResponses(exec, tracker)

	// Message handler: Discord → conspiracy inbox
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore own messages
		if m.Author.ID == s.State.User.ID {
			return
		}
		// Ignore bot messages
		if m.Author.Bot {
			return
		}

		// Channel mode: only respond in the configured channel
		if cfg.ChannelID != "" && m.ChannelID != cfg.ChannelID {
			return
		}

		// DM mode: only respond to DMs
		if cfg.ChannelID == "" {
			ch, err := s.State.Channel(m.ChannelID)
			if err != nil {
				ch, err = s.Channel(m.ChannelID)
				if err != nil {
					return
				}
			}
			if ch.Type != discordgo.ChannelTypeDM {
				return
			}
			dms.add(m.ChannelID)
		}

		// Forward message to conspiracy
		message := m.Content
		if message == "" {
			return
		}

		// Escape single quotes for shell
		escaped := strings.ReplaceAll(message, "'", "'\\''")
		cmd := fmt.Sprintf("conctl task '%s'", escaped)

		_, err := exec.Run(cmd)
		if err != nil {
			log.Printf("task failed: %v", err)
			s.MessageReactionAdd(m.ChannelID, m.ID, "\u274c") // cross mark
			return
		}

		s.MessageReactionAdd(m.ChannelID, m.ID, "\u2705") // check mark
		startTyping(s, m.ChannelID)
		log.Printf("task from %s: %s", m.Author.Username, driverutil.Truncate(message, 80))
	})

	// Interaction handler: slash commands
	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}

		// Track DM channel from interactions too
		if cfg.ChannelID == "" && i.User != nil {
			ch, err := s.UserChannelCreate(i.User.ID)
			if err == nil {
				dms.add(ch.ID)
			}
		}

		data := i.ApplicationCommandData()
		switch data.Name {
		case "status":
			handleStatus(s, i, exec)
		case "clear":
			handleClear(s, i, exec)
		case "logs":
			handleLogs(s, i, exec, data.Options)
		case "responses":
			handleResponses(s, i, exec)
		case "history":
			handleHistory(s, i, exec, data.Options)
		case "debug":
			handleDebug(s, i, cfg, exec, tracker, dms)
		}
	})

	if err := dg.Open(); err != nil {
		log.Fatalf("opening Discord connection: %v", err)
	}
	defer dg.Close()

	// Register slash commands
	for _, cmd := range slashCommands {
		_, err := dg.ApplicationCommandCreate(dg.State.User.ID, "", cmd)
		if err != nil {
			log.Printf("registering /%s: %v", cmd.Name, err)
		} else {
			log.Printf("registered /%s", cmd.Name)
		}
	}

	mode := "DM"
	if cfg.ChannelID != "" {
		mode = fmt.Sprintf("channel %s", cfg.ChannelID)
	}
	log.Printf("discord driver started (%s mode), polling %s@%s:%s",
		mode, cfg.SSH.User, cfg.SSH.Host, cfg.SSH.Port)

	// Start response poller
	go pollResponses(dg, cfg, exec, tracker, dms)

	// Block until signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}

// --- Slash command handlers ---

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	chunks := splitMessage(content, 2000)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: chunks[0],
		},
	})
	// Send overflow as follow-up messages
	for _, chunk := range chunks[1:] {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: chunk,
		})
	}
}

func handleStatus(s *discordgo.Session, i *discordgo.InteractionCreate, exec driverutil.Executor) {
	out, err := exec.Run("conctl status")
	if err != nil {
		respond(s, i, fmt.Sprintf("SSH failed: %v\n%s", err, out))
		return
	}
	respond(s, i, fmt.Sprintf("```\n%s\n```", out))
}

func handleClear(s *discordgo.Session, i *discordgo.InteractionCreate, exec driverutil.Executor) {
	out, err := exec.Run("conctl clear-sessions concierge")
	if err != nil {
		respond(s, i, fmt.Sprintf("Failed to clear session: %v\n%s", err, out))
		return
	}
	respond(s, i, fmt.Sprintf("Session cleared: %s", strings.TrimSpace(out)))
	log.Printf("session cleared by %s", interactionUser(i))
}

func handleLogs(s *discordgo.Session, i *discordgo.InteractionCreate, exec driverutil.Executor, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	count := 20
	for _, opt := range opts {
		if opt.Name == "count" {
			count = int(opt.IntValue())
		}
	}
	if count < 1 {
		count = 1
	}
	if count > 100 {
		count = 100
	}

	cmd := fmt.Sprintf("conctl logs -n %d", count)
	out, err := exec.Run(cmd)
	if err != nil || out == "" {
		respond(s, i, "No audit log entries found.")
		return
	}
	respond(s, i, fmt.Sprintf("```\n%s\n```", out))
}

func handleResponses(s *discordgo.Session, i *discordgo.InteractionCreate, exec driverutil.Executor) {
	out, err := exec.Run("conctl responses")
	if err != nil {
		respond(s, i, fmt.Sprintf("SSH failed: %v\n%s", err, out))
		return
	}
	if out == "" {
		respond(s, i, "No responses found.")
		return
	}
	respond(s, i, out)
}

func handleHistory(s *discordgo.Session, i *discordgo.InteractionCreate, exec driverutil.Executor, _ []*discordgo.ApplicationCommandInteractionDataOption) {
	// count option ignored — conctl responses shows latest from each agent
	out, err := exec.Run("conctl responses")
	if err != nil || out == "" {
		respond(s, i, "No response history found.")
		return
	}
	respond(s, i, out)
}

func handleDebug(s *discordgo.Session, i *discordgo.InteractionCreate, cfg Config, exec driverutil.Executor, tracker *driverutil.ResponseTracker, dms *dmChannels) {
	mode := "DM"
	if cfg.ChannelID != "" {
		mode = fmt.Sprintf("channel %s", cfg.ChannelID)
	}

	uptime := time.Since(startTime).Truncate(time.Second)

	// Test SSH connectivity
	sshStatus := "connected"
	statusOut, err := exec.Run("conctl status")
	if err != nil {
		sshStatus = fmt.Sprintf("FAILED: %v", err)
		statusOut = ""
	}

	// Count pending tasks
	pending := ""
	if statusOut != "" {
		pending = statusOut
	}

	lines := []string{
		"```",
		fmt.Sprintf("Mode:       %s", mode),
		fmt.Sprintf("Target:     %s@%s:%s", cfg.SSH.User, cfg.SSH.Host, cfg.SSH.Port),
		fmt.Sprintf("SSH:        %s", sshStatus),
		fmt.Sprintf("Uptime:     %s", uptime),
		fmt.Sprintf("Seen:       %d responses tracked", tracker.Count()),
		fmt.Sprintf("DM chans:   %d active", dms.count()),
	}
	if pending != "" {
		lines = append(lines, "")
		lines = append(lines, pending)
	}
	lines = append(lines, "```")

	respond(s, i, strings.Join(lines, "\n"))
}

// interactionUser returns the username from an interaction (works in both DM and guild).
func interactionUser(i *discordgo.InteractionCreate) string {
	if i.User != nil {
		return i.User.Username
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.Username
	}
	return "unknown"
}

// --- Response polling ---

// pollResponses checks for new response files and posts them to Discord.
func pollResponses(dg *discordgo.Session, cfg Config, exec driverutil.Executor, tracker *driverutil.ResponseTracker, dms *dmChannels) {
	for {
		time.Sleep(5 * time.Second)

		out, err := exec.Run("ls /srv/conos/agents/*/outbox/*.response 2>/dev/null")
		if err != nil || out == "" {
			continue
		}

		for _, path := range strings.Split(out, "\n") {
			path = strings.TrimSpace(path)
			if path == "" || !tracker.IsNew(path) {
				continue
			}

			// Extract agent name from path: /srv/conos/agents/<name>/outbox/...
			agent := driverutil.AgentFromPath(path)

			content, err := exec.Run(fmt.Sprintf("cat '%s'", path))
			if err != nil {
				log.Printf("reading response %s: %v", path, err)
				continue
			}

			if content == "" {
				continue
			}

			if cfg.BaseURL != "" {
				content = resolveArtifactLinks(content, cfg.BaseURL, exec)
			}
			header := fmt.Sprintf("**%s:**\n", agent)
			sendResponse(dg, cfg, dms, header+content)
			log.Printf("posted response from %s (%d chars)", agent, len(content))
		}
	}
}

// sendResponse posts a message to the appropriate Discord destination.
func sendResponse(dg *discordgo.Session, cfg Config, dms *dmChannels, content string) {
	content = redactSecrets(content, []string{cfg.BotToken})
	chunks := splitMessage(content, 2000)

	if cfg.ChannelID != "" {
		// Channel mode: post to configured channel
		stopTyping(cfg.ChannelID)
		for _, chunk := range chunks {
			dg.ChannelMessageSend(cfg.ChannelID, chunk)
		}
		return
	}

	// DM mode: post to all active DM channels
	for _, chID := range dms.list() {
		stopTyping(chID)
		for _, chunk := range chunks {
			dg.ChannelMessageSend(chID, chunk)
		}
	}
}

// --- Artifact resolution ---

// artifactPattern matches artifact references in response text.
// Matches: artifact:art_<hex>, /artifacts/art_<hex>/<filename>
var artifactPattern = regexp.MustCompile(`(?:artifact:|/artifacts/)(art_[0-9a-f]{16})(?:/([A-Za-z0-9._-]+))?`)

// artifactLinkResponse is the JSON shape returned by `conctl artifact link`.
type artifactLinkResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// resolveArtifactLinks finds artifact references in text and replaces them
// with signed URLs by calling `conctl artifact link <id>` via SSH.
func resolveArtifactLinks(text string, baseURL string, exec driverutil.Executor) string {
	matches := artifactPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	// Deduplicate: resolve each unique artifact ID once
	resolved := make(map[string]string) // artifactID -> signed URL

	for _, match := range matches {
		artID := text[match[2]:match[3]]
		if _, ok := resolved[artID]; ok {
			continue
		}

		cmd := fmt.Sprintf("conctl artifact link --base-url '%s' %s", baseURL, artID)
		out, err := exec.Run(cmd)
		if err != nil {
			log.Printf("artifact link %s failed: %v", artID, err)
			resolved[artID] = "" // mark as failed, leave original
			continue
		}

		var resp artifactLinkResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			log.Printf("artifact link %s: invalid JSON: %v", artID, err)
			resolved[artID] = ""
			continue
		}
		resolved[artID] = resp.URL
	}

	// Replace matches in reverse order to preserve indices
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		artID := text[match[2]:match[3]]
		url := resolved[artID]
		if url == "" {
			continue // leave original text unchanged on failure
		}
		text = text[:match[0]] + url + text[match[1]:]
	}

	return text
}

// --- Helpers ---

// splitMessage splits content into chunks that fit Discord's 2000 char limit.
func splitMessage(content string, limit int) []string {
	if len(content) <= limit {
		return []string{content}
	}
	var chunks []string
	for len(content) > 0 {
		end := limit
		if end > len(content) {
			end = len(content)
		}
		// Try to split on newline
		if end < len(content) {
			if idx := strings.LastIndex(content[:end], "\n"); idx > 0 {
				end = idx + 1
			}
		}
		chunks = append(chunks, content[:end])
		content = content[end:]
	}
	return chunks
}


// secretPatterns matches API key formats that must not be forwarded to Discord.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9\-]{20,}`),    // OpenRouter / OpenAI / Anthropic sk- keys
	regexp.MustCompile(`tskey-[A-Za-z0-9\-]{10,}`), // Tailscale auth keys
}

// redactSecrets replaces recognised secret patterns and any provided literal
// values in s with "[REDACTED]". literalSecrets is typically the bot token
// or other config values that must not echo back to the channel.
func redactSecrets(s string, literalSecrets []string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	for _, secret := range literalSecrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return s
}
