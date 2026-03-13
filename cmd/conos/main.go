package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ConspiracyOS/conos/internal/commands"
	"github.com/ConspiracyOS/conos/internal/config"
	"github.com/ConspiracyOS/conos/internal/runner"
	rt "github.com/ConspiracyOS/conos/internal/runtime"
)

const defaultImage = "ghcr.io/conspiracyos/conos:latest"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "install":
		cmdInstall(os.Args[2:])
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "status":
		cmdStatus()
	case "config":
		if len(os.Args) < 3 || os.Args[2] != "apply" {
			fatalf("usage: conos config apply\n")
		}
		cmdConfigApply()
	case "agent":
		cmdAgent(os.Args[2:])
	default:
		fatalf("unknown command: %s\n", os.Args[1])
	}
}

func mustLoadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fatalf("config: %v\n", err)
	}
	return cfg
}

func loadContainerConfigOrDefault() config.Container {
	c, err := config.LoadContainer()
	if err != nil {
		fatalf("config: %v\n", err)
	}
	return c
}

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	runtimeFlag := fs.String("runtime", "", "container runtime: docker|podman")
	nameFlag := fs.String("name", "", "container name")
	imageFlag := fs.String("image", "", "container image")
	envFileFlag := fs.String("env-file", "", "runtime env file path")
	sshPortFlag := fs.Int("ssh-port", 0, "host port for SSH access")
	apiKeyFlag := fs.String("api-key", "", "LLM API key (avoids interactive prompt)")
	fs.Parse(args)

	// Run the wizard — interactive if TTY, silent otherwise.
	// Flags override wizard prompts.
	wiz := NewWizard(nil, nil)
	opts := wiz.Run(InstallOpts{
		Runtime: *runtimeFlag,
		Name:    *nameFlag,
		Image:   *imageFlag,
		EnvFile: *envFileFlag,
		SSHPort: *sshPortFlag,
		APIKey:  *apiKeyFlag,
	})

	// Write env file
	writeEnvFile(opts.EnvFile, opts.APIKey)

	cfg := rt.ContainerConfig{
		Runtime: opts.Runtime,
		Name:    opts.Name,
		Image:   opts.Image,
		EnvFile: opts.EnvFile,
		SSHPort: opts.SSHPort,
	}

	// 1. Pull image
	fmt.Printf("Pulling image %s...\n", cfg.Image)
	if err := rt.Pull(cfg); err != nil {
		fatalf("install failed: image pull failed: %v\n", err)
	}

	// 2. Start container
	fmt.Printf("Replacing container %s if it exists...\n", cfg.Name)
	rt.RemoveIfExists(cfg)

	fmt.Printf("Starting %s...\n", cfg.Name)
	if err := rt.Start(cfg); err != nil {
		fatalf("install failed: start failed: %v\n", err)
	}

	// 3. Inject SSH key
	fmt.Println("Configuring SSH access...")
	if err := rt.InjectSSHKey(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: SSH setup failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "You can manually copy your SSH key later with:")
		fmt.Fprintf(os.Stderr, "  docker exec %s bash -c 'mkdir -p /root/.ssh && cat >> /root/.ssh/authorized_keys' < ~/.ssh/id_ed25519.pub\n", cfg.Name)
	} else {
		fmt.Println("SSH key injected.")
	}

	// 4. Generate config
	if err := generateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config generation failed: %v\n", err)
	}

	fmt.Println()
	fmt.Println("Install complete.")
	if opts.APIKey == "" {
		fmt.Println()
		fmt.Println("  Agents won't be able to think until you add an API key:")
		fmt.Println("    echo 'CONOS_API_KEY=sk-your-key' >>", cfg.EnvFile)
		fmt.Println("    conos stop --force && conos start")
	}
	fmt.Println()
	fmt.Println("Try:")
	fmt.Println("  conos status")
	fmt.Println("  conos agent \"What agents are running?\"")
}

func generateConfig(cfg rt.ContainerConfig) error {
	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".conos")
	configPath := filepath.Join(configDir, "conos.toml")

	// Don't overwrite existing config
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}

	content := fmt.Sprintf(`[instance]
host = "conos"

[container]
runtime = %q
name = %q
image = %q
env_file = %q
`, cfg.Runtime, cfg.Name, cfg.Image, cfg.EnvFile)

	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return err
	}

	fmt.Printf("Config written to %s\n", configPath)

	// Also set up SSH config entry if it doesn't exist
	return ensureSSHConfig(cfg)
}

func ensureSSHConfig(cfg rt.ContainerConfig) error {
	home := os.Getenv("HOME")
	sshConfigPath := filepath.Join(home, ".ssh", "config")

	// Check if "Host conos" already exists
	existing, err := os.ReadFile(sshConfigPath)
	if err == nil && strings.Contains(string(existing), "Host "+cfg.Name) {
		return nil
	}

	entry := fmt.Sprintf(`
# ConspiracyOS (added by conos install)
Host %s
  HostName 127.0.0.1
  Port %d
  User root
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  LogLevel ERROR
`, cfg.Name, cfg.SSHPort)

	f, err := os.OpenFile(sshConfigPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("writing SSH config: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return err
	}

	fmt.Printf("SSH config entry added for host %q (port %d)\n", cfg.Name, cfg.SSHPort)
	return nil
}

func writeEnvFile(path string, apiKey string) {
	if _, err := os.Stat(path); err == nil {
		return // already exists
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}

	if apiKey != "" {
		content := "CONOS_API_KEY=" + apiKey + "\n"
		_ = os.WriteFile(path, []byte(content), 0o600)
	} else {
		// Create empty env file so docker --env-file doesn't error
		_ = os.WriteFile(path, []byte("# Set your LLM API key:\n# CONOS_API_KEY=sk-your-key-here\n"), 0o600)
	}
}

func cmdStart() {
	containerCfg := loadContainerConfigOrDefault()
	cfg := rt.ContainerConfig{
		Runtime: containerCfg.Runtime,
		Name:    containerCfg.Name,
		Image:   containerCfg.Image,
		EnvFile: containerCfg.EnvFile,
	}
	fmt.Printf("Starting %s...\n", cfg.Name)
	if err := rt.Start(cfg); err != nil {
		fatalf("start failed: %v\n", err)
	}
}

func cmdStop() {
	force := false
	for _, arg := range os.Args[2:] {
		if arg == "--force" {
			force = true
		}
	}
	containerCfg := loadContainerConfigOrDefault()
	cfg := rt.ContainerConfig{
		Runtime: containerCfg.Runtime,
		Name:    containerCfg.Name,
	}
	if err := rt.Stop(cfg, force); err != nil {
		fatalf("stop failed: %v\n", err)
	}
}

func cmdStatus() {
	cfg := mustLoadConfig()
	r := &runner.SSHRunner{Host: cfg.Instance.Host}
	out, err := commands.Status(r)
	if err != nil {
		fatalf("status failed: %v\n%s\n", err, out)
	}
	fmt.Println(out)
}

func cmdConfigApply() {
	cfg := mustLoadConfig()
	localPath := ".conos/conos.toml"
	if _, err := os.Stat(localPath); err != nil {
		fatalf("config file not found: %s\n", localPath)
	}
	fmt.Printf("Copying %s to %s:/etc/conos/conos.toml...\n", localPath, cfg.Instance.Host)
	if err := commands.ConfigApply(cfg.Instance.Host, localPath); err != nil {
		fatalf("config apply failed: %v\n", err)
	}
	fmt.Println("config applied")
}

func cmdAgent(args []string) {
	if len(args) == 0 {
		fatalf("usage: conos agent <task> | list | kill <name> | logs [flags] [agent] | task <name> <task>\n")
	}
	cfg := mustLoadConfig()
	r := &runner.SSHRunner{Host: cfg.Instance.Host}
	switch args[0] {
	case "list":
		out, err := commands.AgentList(r)
		if err != nil {
			fatalf("agent list failed: %v\n%s\n", err, out)
		}
		fmt.Println(out)
	case "task":
		if len(args) < 3 {
			fatalf("usage: conos agent task <name> <task>\n")
		}
		name := args[1]
		task := strings.Join(args[2:], " ")
		if err := commands.AgentSendTo(r, name, task); err != nil {
			fatalf("agent task failed: %v\n", err)
		}
		fmt.Printf("task sent to %s\n", name)
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
	case "kill":
		if len(args) < 2 {
			fatalf("usage: conos agent kill <name>\n")
		}
		if err := commands.AgentKill(r, args[1]); err != nil {
			fatalf("agent kill failed: %v\n", err)
		}
		fmt.Printf("killed %s\n", args[1])
	default:
		// Treat all args as the task message to concierge
		task := strings.Join(args, " ")
		if err := commands.AgentSend(r, task); err != nil {
			fatalf("agent send failed: %v\n", err)
		}
		fmt.Println("task sent")
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `conos — ConspiracyOS outer CLI

Usage:
  conos install [--runtime docker] [--name conos] [--image ghcr.io/conspiracyos/conos:latest]
                                         install + start locally (creates ~/.conos/container.env if missing)
  conos start                           boot the instance
  conos stop [--force]                  stop the instance
  conos status                          show agent status
  conos config apply                    push config to instance
  conos agent list                      list agents
  conos agent kill <name>               stop a running agent
  conos agent logs [-f] [-n N] [name]   tail audit log
  conos agent task <name> <task>        send task to named agent
  conos agent <task>                    send task to concierge
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
