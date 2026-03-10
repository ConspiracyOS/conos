package commands

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ConspiracyOS/conos/internal/runner"
)

var validAgentName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func checkAgentName(name string) error {
	if !validAgentName.MatchString(name) {
		return fmt.Errorf("invalid agent name %q: must match [a-z][a-z0-9-]*", name)
	}
	return nil
}

// Status shows agent status from the instance.
func Status(r runner.Runner) (string, error) {
	return r.Exec("conctl status")
}

// AgentList lists agents and their state (same output as status).
func AgentList(r runner.Runner) (string, error) {
	return r.Exec("conctl status")
}

// AgentSend sends a task to the concierge (outer inbox).
func AgentSend(r runner.Runner, task string) error {
	_, err := r.Exec("conctl task '" + shellEscape(task) + "'")
	return err
}

// AgentSendTo sends a task directly to a named agent's inbox.
func AgentSendTo(r runner.Runner, agentName, task string) error {
	if err := checkAgentName(agentName); err != nil {
		return err
	}
	_, err := r.Exec("conctl task --agent " + agentName + " '" + shellEscape(task) + "'")
	return err
}

// shellEscape escapes single quotes for use in a single-quoted shell string.
func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// AgentLogs streams or shows the audit log.
// When follow is true, streams via Stream (interactive); otherwise captures and prints.
func AgentLogs(r runner.Runner, follow bool, n int, agent string) error {
	if agent != "" {
		if err := checkAgentName(agent); err != nil {
			return err
		}
	}
	cmd := "conctl logs"
	if follow {
		cmd += fmt.Sprintf(" -f -n %d", n)
	} else if n != 20 || agent != "" {
		cmd += fmt.Sprintf(" -n %d", n)
	}
	if agent != "" {
		cmd += " " + agent
	}
	if follow {
		return r.Stream(cmd)
	}
	out, err := r.Exec(cmd)
	if out != "" {
		fmt.Println(out)
	}
	return err
}

// AgentKill stops the named agent's systemd units.
func AgentKill(r runner.Runner, name string) error {
	if err := checkAgentName(name); err != nil {
		return err
	}
	_, err := r.Exec("conctl kill " + name)
	return err
}

// BuildScpArgs constructs the scp argument list. Exported for testing.
func BuildScpArgs(host, localPath string) []string {
	return []string{"scp", localPath, host + ":/etc/conos/conos.toml"}
}

// ConfigApply copies the local conos.toml to the instance and runs bootstrap.
func ConfigApply(host, localPath string) error {
	// Step 1: clear immutable bit so scp can overwrite
	unlock := exec.Command("ssh", host, "chattr -i /etc/conos/conos.toml 2>/dev/null; true")
	unlock.Stdout = os.Stdout
	unlock.Stderr = os.Stderr
	_ = unlock.Run() // best-effort: may not have chattr or immutable support

	// Step 2: scp
	scpArgs := BuildScpArgs(host, localPath)
	scp := exec.Command(scpArgs[0], scpArgs[1:]...)
	scp.Stdout = os.Stdout
	scp.Stderr = os.Stderr
	if err := scp.Run(); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}

	// Step 3: conctl bootstrap (re-applies immutable bit as its last step)
	bootstrap := exec.Command("ssh", host, "conctl bootstrap")
	bootstrap.Stdout = os.Stdout
	bootstrap.Stderr = os.Stderr
	return bootstrap.Run()
}
