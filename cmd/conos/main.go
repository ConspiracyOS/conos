package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ConspiracyOS/conos/internal/commands"
	"github.com/ConspiracyOS/conos/internal/config"
	rt "github.com/ConspiracyOS/conos/internal/runtime"
	"github.com/ConspiracyOS/conos/internal/runner"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
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

func cmdStart() {
	cfg := mustLoadConfig()
	c := rt.ContainerConfig{
		Runtime: cfg.Container.Runtime,
		Name:    cfg.Container.Name,
		Image:   cfg.Container.Image,
		EnvFile: cfg.Container.EnvFile,
	}
	fmt.Printf("Starting %s...\n", c.Name)
	if err := rt.Start(c); err != nil {
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
	cfg := mustLoadConfig()
	c := rt.ContainerConfig{
		Runtime: cfg.Container.Runtime,
		Name:    cfg.Container.Name,
	}
	if err := rt.Stop(c, force); err != nil {
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
