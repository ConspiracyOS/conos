package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

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
	SSHHost   string
	SSHPort   string
	SSHUser   string
	SSHKey    string
}

func loadConfig() Config {
	cfg := Config{
		BotToken:  os.Getenv("DISCORD_BOT_TOKEN"),
		ChannelID: os.Getenv("DISCORD_CHANNEL_ID"),
		SSHHost:   os.Getenv("CON_SSH_HOST"),
		SSHPort:   os.Getenv("CON_SSH_PORT"),
		SSHUser:   os.Getenv("CON_SSH_USER"),
		SSHKey:    os.Getenv("CON_SSH_KEY"),
	}
	if cfg.BotToken == "" {
		log.Fatal("DISCORD_BOT_TOKEN is required")
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
		"-o", "StrictHostKeyChecking=no",
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
	return true
}

func (rt *responseTracker) count() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.seen)
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

	exec := &SSHExecutor{Config: cfg}
	tracker := newResponseTracker()
	dms := newDMChannels()

	// Seed the tracker with existing responses so we only post new ones
	seedResponses(exec, tracker)

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
		cmd := fmt.Sprintf("con task '%s'", escaped)

		_, err := exec.Run(cmd)
		if err != nil {
			log.Printf("task failed: %v", err)
			s.MessageReactionAdd(m.ChannelID, m.ID, "\u274c") // cross mark
			return
		}

		s.MessageReactionAdd(m.ChannelID, m.ID, "\u2705") // check mark
		startTyping(s, m.ChannelID)
		log.Printf("task from %s: %s", m.Author.Username, truncate(message, 80))
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
		mode, cfg.SSHUser, cfg.SSHHost, cfg.SSHPort)

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

func handleStatus(s *discordgo.Session, i *discordgo.InteractionCreate, exec Executor) {
	out, err := exec.Run("con status")
	if err != nil {
		respond(s, i, fmt.Sprintf("SSH failed: %v\n%s", err, out))
		return
	}
	respond(s, i, fmt.Sprintf("```\n%s\n```", out))
}

func handleClear(s *discordgo.Session, i *discordgo.InteractionCreate, exec Executor) {
	out, err := exec.Run("rm -f /srv/con/agents/concierge/workspace/sessions/*.json")
	if err != nil {
		respond(s, i, fmt.Sprintf("Failed to clear session: %v\n%s", err, out))
		return
	}
	respond(s, i, "Concierge session cleared.")
	log.Printf("session cleared by %s", interactionUser(i))
}

func handleLogs(s *discordgo.Session, i *discordgo.InteractionCreate, exec Executor, opts []*discordgo.ApplicationCommandInteractionDataOption) {
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

	cmd := fmt.Sprintf("tail -n %d /srv/con/logs/audit/*.log 2>/dev/null", count)
	out, err := exec.Run(cmd)
	if err != nil || out == "" {
		respond(s, i, "No audit log entries found.")
		return
	}
	respond(s, i, fmt.Sprintf("```\n%s\n```", out))
}

func handleResponses(s *discordgo.Session, i *discordgo.InteractionCreate, exec Executor) {
	out, err := exec.Run("con responses")
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

func handleHistory(s *discordgo.Session, i *discordgo.InteractionCreate, exec Executor, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	count := 5
	for _, opt := range opts {
		if opt.Name == "count" {
			count = int(opt.IntValue())
		}
	}
	if count < 1 {
		count = 1
	}
	if count > 20 {
		count = 20
	}

	cmd := fmt.Sprintf(
		`ls -t /srv/con/agents/*/outbox/*.response 2>/dev/null | head -%d | while read f; do agent=$(echo "$f" | awk -F/ '{print $5}'); echo "**${agent}** ($(basename "$f"))"; cat "$f"; echo; echo "---"; done`,
		count,
	)
	out, err := exec.Run(cmd)
	if err != nil || out == "" {
		respond(s, i, "No response history found.")
		return
	}
	respond(s, i, out)
}

func handleDebug(s *discordgo.Session, i *discordgo.InteractionCreate, cfg Config, exec Executor, tracker *responseTracker, dms *dmChannels) {
	mode := "DM"
	if cfg.ChannelID != "" {
		mode = fmt.Sprintf("channel %s", cfg.ChannelID)
	}

	uptime := time.Since(startTime).Truncate(time.Second)

	// Test SSH connectivity
	sshStatus := "connected"
	statusOut, err := exec.Run("con status")
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
		fmt.Sprintf("Target:     %s@%s:%s", cfg.SSHUser, cfg.SSHHost, cfg.SSHPort),
		fmt.Sprintf("SSH:        %s", sshStatus),
		fmt.Sprintf("Uptime:     %s", uptime),
		fmt.Sprintf("Seen:       %d responses tracked", tracker.count()),
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

// seedResponses marks all existing response files as seen so we don't replay history.
func seedResponses(exec Executor, tracker *responseTracker) {
	out, err := exec.Run("ls /srv/con/agents/*/outbox/*.response 2>/dev/null")
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

// pollResponses checks for new response files and posts them to Discord.
func pollResponses(dg *discordgo.Session, cfg Config, exec Executor, tracker *responseTracker, dms *dmChannels) {
	for {
		time.Sleep(5 * time.Second)

		out, err := exec.Run("ls /srv/con/agents/*/outbox/*.response 2>/dev/null")
		if err != nil || out == "" {
			continue
		}

		for _, path := range strings.Split(out, "\n") {
			path = strings.TrimSpace(path)
			if path == "" || !tracker.isNew(path) {
				continue
			}

			// Extract agent name from path: /srv/con/agents/<name>/outbox/...
			agent := agentFromPath(path)

			content, err := exec.Run(fmt.Sprintf("cat '%s'", path))
			if err != nil {
				log.Printf("reading response %s: %v", path, err)
				continue
			}

			if content == "" {
				continue
			}

			header := fmt.Sprintf("**%s:**\n", agent)
			sendResponse(dg, cfg, dms, header+content)
			log.Printf("posted response from %s (%d chars)", agent, len(content))
		}
	}
}

// sendResponse posts a message to the appropriate Discord destination.
func sendResponse(dg *discordgo.Session, cfg Config, dms *dmChannels, content string) {
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

// agentFromPath extracts agent name from /srv/con/agents/<name>/outbox/...
func agentFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "agents" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "unknown"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
