package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ConspiracyOS/conos/internal/commands"
	"github.com/ConspiracyOS/conos/internal/config"
	"github.com/ConspiracyOS/conos/internal/runner"
	rt "github.com/ConspiracyOS/conos/internal/runtime"
)

const defaultImage = "ghcr.io/conspiracyos/conos:latest"

// reservedCommands are command names that cannot be used as target names.
var reservedCommands = map[string]bool{
	"install": true, "start": true, "stop": true, "status": true,
	"task": true, "agent": true, "config": true, "targets": true,
	"bootstrap": true, "doctor": true, "help": true,
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	// Resolve target: if first arg is a known target name, use it and shift args.
	// Otherwise use the default target.
	arg1 := os.Args[1]
	var targetName string
	var cmdArgs []string

	if !reservedCommands[arg1] && len(os.Args) > 2 {
		// Could be a target name — check config
		cfg, err := config.Load()
		if err == nil {
			if _, found := cfg.FindTarget(arg1); found {
				targetName = arg1
				cmdArgs = os.Args[2:]
			}
		}
	}

	if cmdArgs == nil {
		cmdArgs = os.Args[1:]
	}

	if len(cmdArgs) == 0 {
		usage()
		os.Exit(1)
	}

	switch cmdArgs[0] {
	case "install":
		cmdInstall(cmdArgs[1:])
	case "start":
		cmdStart(targetName)
	case "stop":
		cmdStop(targetName, cmdArgs[1:])
	case "status":
		cmdStatus(targetName)
	case "task":
		cmdTask(targetName, cmdArgs[1:])
	case "agent":
		cmdAgent(targetName, cmdArgs[1:])
	case "config":
		if len(cmdArgs) < 2 || cmdArgs[1] != "apply" {
			fatalf("usage: conos [target] config apply\n")
		}
		cmdConfigApply(targetName)
	case "targets":
		cmdTargets()
	case "bootstrap":
		cmdBootstrap(targetName, cmdArgs[1:])
	case "doctor":
		cmdDoctor(targetName)
	default:
		fatalf("unknown command: %s\n", cmdArgs[0])
	}
}

func resolveTarget(name string) *config.Target {
	cfg, err := config.Load()
	if err != nil {
		fatalf("config: %v\n", err)
	}
	if name != "" {
		t, found := cfg.FindTarget(name)
		if !found {
			fatalf("target %q not found\n", name)
		}
		return t
	}
	t, err := cfg.DefaultTarget()
	if err != nil {
		fatalf("%v\n", err)
	}
	return t
}

func runnerForTarget(t *config.Target) runner.Runner {
	r, err := runner.ForTarget(t.Transport, t.Host, t.Runtime, t.Container)
	if err != nil {
		fatalf("transport error for %s: %v\n", t.Name, err)
	}
	return r
}

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	nameFlag := fs.String("name", "conos", "target name")
	transportFlag := fs.String("transport", "container", "transport: container|ssh|local")
	runtimeFlag := fs.String("runtime", "docker", "container runtime: docker|podman")
	imageFlag := fs.String("image", defaultImage, "container image")
	hostFlag := fs.String("host", "", "SSH host (for ssh/container transport)")
	envFileFlag := fs.String("env-file", "", "runtime env file path")
	sshPortFlag := fs.Int("ssh-port", 2222, "host port for SSH access")
	apiKeyFlag := fs.String("api-key", "", "LLM API key")
	setDefault := fs.Bool("default", false, "set as default target")
	fs.Parse(args)

	name := *nameFlag
	if reservedCommands[name] {
		fatalf("target name %q conflicts with a command name\n", name)
	}

	t := config.Target{
		Name:      name,
		Transport: *transportFlag,
		Default:   *setDefault,
		Runtime:   *runtimeFlag,
		Container: name,
		Host:      *hostFlag,
		Image:     *imageFlag,
		SSHPort:   *sshPortFlag,
	}

	// Set env file default
	envFile := *envFileFlag
	if envFile == "" && t.Transport == "container" {
		home := os.Getenv("HOME")
		envFile = home + "/.conos/" + name + ".env"
	}
	t.EnvFile = envFile

	// For container transport: run the container install flow
	if t.Transport == "container" {
		if t.Host == "" {
			t.Host = name
		}

		// Write env file
		writeEnvFile(t.EnvFile, *apiKeyFlag)

		cfg := rt.ContainerConfig{
			Runtime: t.Runtime,
			Name:    t.Container,
			Image:   t.Image,
			EnvFile: t.EnvFile,
			SSHPort: t.SSHPort,
		}

		fmt.Printf("Pulling image %s...\n", cfg.Image)
		if err := rt.Pull(cfg); err != nil {
			fatalf("install failed: image pull failed: %v\n", err)
		}

		fmt.Printf("Replacing container %s if it exists...\n", cfg.Name)
		rt.RemoveIfExists(cfg)

		fmt.Printf("Starting %s...\n", cfg.Name)
		if err := rt.Start(cfg); err != nil {
			fatalf("install failed: start failed: %v\n", err)
		}

		fmt.Println("Configuring SSH access...")
		if err := rt.InjectSSHKey(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: SSH setup failed: %v\n", err)
		} else {
			fmt.Println("SSH key injected.")
		}

		if err := ensureSSHConfig(name, t.SSHPort); err != nil {
			fmt.Fprintf(os.Stderr, "warning: SSH config: %v\n", err)
		}
	}

	// Register the target
	if err := config.AddTarget(t); err != nil {
		fatalf("saving target: %v\n", err)
	}

	fmt.Printf("\nTarget %q added.\n", name)
	if t.Transport == "container" && *apiKeyFlag == "" {
		fmt.Printf("\n  Agents need an API key:\n    echo 'CONOS_API_KEY=sk-your-key' >> %s\n    conos %s stop --force && conos %s start\n", t.EnvFile, name, name)
	}
	fmt.Printf("\nTry:\n  conos %s status\n  conos %s task \"What agents are running?\"\n", name, name)
}

func cmdStart(targetName string) {
	t := resolveTarget(targetName)
	if t.Transport != "container" {
		fatalf("start is only supported for container targets (target %q uses %s)\n", t.Name, t.Transport)
	}
	cfg := rt.ContainerConfig{
		Runtime: t.Runtime,
		Name:    t.Container,
		Image:   t.Image,
		EnvFile: t.EnvFile,
		SSHPort: t.SSHPort,
		Mounts:  t.Mounts,
	}
	fmt.Printf("Starting %s...\n", cfg.Name)
	if err := rt.Start(cfg); err != nil {
		fatalf("start failed: %v\n", err)
	}
	if err := rt.InjectSSHKey(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: SSH setup failed after start: %v\n", err)
	}
}

func cmdStop(targetName string, args []string) {
	force := false
	for _, arg := range args[1:] {
		if arg == "--force" {
			force = true
		}
	}
	t := resolveTarget(targetName)
	if t.Transport != "container" {
		fatalf("stop is only supported for container targets (target %q uses %s)\n", t.Name, t.Transport)
	}
	cfg := rt.ContainerConfig{
		Runtime: t.Runtime,
		Name:    t.Container,
	}
	if err := rt.Stop(cfg, force); err != nil {
		fatalf("stop failed: %v\n", err)
	}
}

func cmdStatus(targetName string) {
	t := resolveTarget(targetName)
	r := runnerForTarget(t)
	out, err := commands.Status(r)
	if err != nil {
		fatalf("status failed: %v\n%s\n", err, out)
	}
	fmt.Println(out)
}

func cmdTask(targetName string, args []string) {
	fs := flag.NewFlagSet("task", flag.ExitOnError)
	agentFlag := fs.String("agent", "", "target agent (default: concierge)")
	threadID := fs.String("thread-id", "", "thread ID for conversation continuity")
	from := fs.String("from", "", "sender identity")
	channel := fs.String("channel", "", "channel name")
	transport := fs.String("transport", "", "transport name")
	source := fs.String("source", "", "source system")
	parentRunID := fs.String("parent-run-id", "", "parent run ID for delegation tracing")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fatalf("usage: conos [target] task [--agent name] [--thread-id id] [--from src] <message>\n")
	}

	t := resolveTarget(targetName)
	r := runnerForTarget(t)

	meta := commands.TaskMeta{
		ThreadID:    *threadID,
		From:        *from,
		Channel:     *channel,
		Transport:   *transport,
		Source:      *source,
		ParentRunID: *parentRunID,
	}

	// If --agent is set, route directly. Otherwise check if first positional
	// arg looks like an agent name (no dashes prefix, no spaces).
	agent := *agentFlag
	message := strings.Join(fs.Args(), " ")

	if agent == "" && fs.NArg() >= 2 {
		first := fs.Arg(0)
		if !strings.HasPrefix(first, "-") {
			agent = first
			message = strings.Join(fs.Args()[1:], " ")
		}
	}

	if agent != "" {
		if err := commands.AgentSendToWithMeta(r, agent, message, meta); err != nil {
			fatalf("task failed: %v\n", err)
		}
		fmt.Printf("task sent to %s\n", agent)
	} else {
		if err := commands.AgentSendWithMeta(r, message, meta); err != nil {
			fatalf("task failed: %v\n", err)
		}
		fmt.Println("task sent")
	}
}

func cmdAgent(targetName string, args []string) {
	if len(args) == 0 {
		fatalf("usage: conos [target] agent <list|kill|logs>\n")
	}
	t := resolveTarget(targetName)
	r := runnerForTarget(t)

	switch args[0] {
	case "list":
		out, err := commands.AgentList(r)
		if err != nil {
			fatalf("agent list failed: %v\n%s\n", err, out)
		}
		fmt.Println(out)
	case "kill":
		if len(args) < 2 {
			fatalf("usage: conos [target] agent kill <name>\n")
		}
		if err := commands.AgentKill(r, args[1]); err != nil {
			fatalf("agent kill failed: %v\n", err)
		}
		fmt.Printf("killed %s\n", args[1])
	case "logs":
		fs := flag.NewFlagSet("logs", flag.ExitOnError)
		follow := fs.Bool("f", false, "follow")
		n := fs.Int("n", 20, "lines")
		fs.Parse(args[1:])
		agent := ""
		if fs.NArg() > 0 {
			agent = fs.Arg(0)
		}
		if err := commands.AgentLogs(r, *follow, *n, agent); err != nil {
			fatalf("agent logs failed: %v\n", err)
		}
	default:
		fatalf("unknown agent command: %s\n", args[0])
	}
}

func cmdConfigApply(targetName string) {
	t := resolveTarget(targetName)
	localPath := ".conos/conos.toml"
	if _, err := os.Stat(localPath); err != nil {
		fatalf("config file not found: %s\n", localPath)
	}
	fmt.Printf("Applying config to %s...\n", t.Name)
	if err := commands.ConfigApply(t.Host, localPath); err != nil {
		fatalf("config apply failed: %v\n", err)
	}
	fmt.Println("config applied")
}

func cmdTargets() {
	cfg, err := config.Load()
	if err != nil {
		fatalf("config: %v\n", err)
	}
	for _, t := range cfg.Targets {
		def := ""
		if t.Default {
			def = " (default)"
		}
		fmt.Printf("%-16s %-12s %s%s\n", t.Name, t.Transport, targetDetail(t), def)
	}
}

func targetDetail(t config.Target) string {
	switch t.Transport {
	case "container":
		return fmt.Sprintf("%s/%s", t.Runtime, t.Container)
	case "ssh":
		return t.Host
	case "local":
		return "localhost"
	default:
		return t.Transport
	}
}

func cmdBootstrap(targetName string, args []string) {
	t := resolveTarget(targetName)
	r := runnerForTarget(t)

	// Preflight: check conctl is available
	fmt.Printf("Bootstrapping %s (%s)...\n\n", t.Name, t.Transport)

	out, err := r.Exec("command -v conctl")
	if err != nil || out == "" {
		fatalf("conctl not found on target %s. Install it first:\n  scp conctl-linux-amd64 %s:/usr/local/bin/conctl\n", t.Name, t.Host)
	}

	// Run bootstrap (with optional flags passthrough)
	cmd := "conctl bootstrap"
	for _, arg := range args {
		cmd += " " + arg
	}

	// Stream so the user sees progress
	if err := r.Stream(cmd); err != nil {
		fatalf("bootstrap failed: %v\n", err)
	}

	fmt.Printf("\nBootstrap complete. Try:\n  conos %s status\n", t.Name)
}

func cmdDoctor(targetName string) {
	t := resolveTarget(targetName)
	r := runnerForTarget(t)

	fmt.Printf("Target: %s (%s)\n", t.Name, t.Transport)

	// Check connectivity
	out, err := r.Exec("conctl status")
	if err != nil {
		fmt.Printf("Connectivity: FAIL (%v)\n", err)
		if out != "" {
			fmt.Printf("Output: %s\n", out)
		}
		return
	}
	fmt.Println("Connectivity: OK")
	fmt.Println()
	fmt.Println(out)
}

func usage() {
	fmt.Fprint(os.Stderr, `conos -- ConspiracyOS outer CLI

Usage:
  conos <command> [args]                  Run against default target
  conos <target> <command> [args]         Run against named target

Instance management:
  conos install [flags]                   Install + register a target
  conos start                             Boot the instance
  conos stop [--force]                    Stop the instance
  conos targets                           List all targets
  conos bootstrap [--sidecar] [--prune]   Run conctl bootstrap on target
  conos doctor                            Connectivity checks

Agent operations:
  conos status                            Agent status
  conos task [<agent>] [flags] <message>  Send task (concierge if no agent)
        --agent, --thread-id, --from, --channel, --transport, --source, --parent-run-id
  conos agent list                        List agents
  conos agent kill <name>                 Stop a running agent
  conos agent logs [-f] [-n N] [name]     Tail audit log

Config:
  conos config apply                      Push local config to instance
`)
}

func ensureSSHConfig(name string, port int) error {
	home := os.Getenv("HOME")
	sshConfigPath := home + "/.ssh/config"

	existing, err := os.ReadFile(sshConfigPath)
	if err == nil && strings.Contains(string(existing), "Host "+name) {
		return nil
	}

	entry := fmt.Sprintf(`
# ConspiracyOS %s (added by conos install)
Host %s
  HostName 127.0.0.1
  Port %d
  User root
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  LogLevel ERROR
`, name, name, port)

	f, err := os.OpenFile(sshConfigPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("writing SSH config: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

func writeEnvFile(path string, apiKey string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.MkdirAll(os.Getenv("HOME")+"/.conos", 0700); err != nil {
		return
	}
	if apiKey != "" {
		_ = os.WriteFile(path, []byte("CONOS_API_KEY="+apiKey+"\n"), 0600)
	} else {
		_ = os.WriteFile(path, []byte("# Set your LLM API key:\n# CONOS_API_KEY=sk-your-key-here\n"), 0600)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
