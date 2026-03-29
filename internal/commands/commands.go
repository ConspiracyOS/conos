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

// TaskMeta holds optional metadata fields for task submission.
type TaskMeta struct {
	ThreadID    string
	From        string
	Channel     string
	Transport   string
	Source      string
	ParentRunID string
}

func (m TaskMeta) flags() string {
	var parts []string
	if m.ThreadID != "" {
		parts = append(parts, "--thread-id '"+shellEscape(m.ThreadID)+"'")
	}
	if m.From != "" {
		parts = append(parts, "--from '"+shellEscape(m.From)+"'")
	}
	if m.Channel != "" {
		parts = append(parts, "--channel '"+shellEscape(m.Channel)+"'")
	}
	if m.Transport != "" {
		parts = append(parts, "--transport '"+shellEscape(m.Transport)+"'")
	}
	if m.Source != "" {
		parts = append(parts, "--source '"+shellEscape(m.Source)+"'")
	}
	if m.ParentRunID != "" {
		parts = append(parts, "--parent-run-id '"+shellEscape(m.ParentRunID)+"'")
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

// AgentSend sends a task to the concierge (outer inbox).
func AgentSend(r runner.Runner, task string) error {
	return AgentSendWithMeta(r, task, TaskMeta{})
}

// AgentSendWithMeta sends a task to the concierge with metadata.
func AgentSendWithMeta(r runner.Runner, task string, meta TaskMeta) error {
	_, err := r.Exec("conctl task" + meta.flags() + " '" + shellEscape(task) + "'")
	return err
}

// AgentSendTo sends a task directly to a named agent's inbox.
func AgentSendTo(r runner.Runner, agentName, task string) error {
	return AgentSendToWithMeta(r, agentName, task, TaskMeta{})
}

// AgentSendToWithMeta sends a task to a named agent with metadata.
func AgentSendToWithMeta(r runner.Runner, agentName, task string, meta TaskMeta) error {
	if err := checkAgentName(agentName); err != nil {
		return err
	}
	_, err := r.Exec("conctl task --agent " + agentName + meta.flags() + " '" + shellEscape(task) + "'")
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

func BuildConfigFixupCmd() string {
	return "chown root:agents /etc/conos/conos.toml && chmod 640 /etc/conos/conos.toml"
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

	// Step 3: ensure agents can read the config before bootstrap writes units.
	fixup := exec.Command("ssh", host, BuildConfigFixupCmd())
	fixup.Stdout = os.Stdout
	fixup.Stderr = os.Stderr
	if err := fixup.Run(); err != nil {
		return fmt.Errorf("config permissions fixup failed: %w", err)
	}

	// Step 4: conctl bootstrap (re-applies immutable bit as its last step)
	bootstrap := exec.Command("ssh", host, "conctl bootstrap")
	bootstrap.Stdout = os.Stdout
	bootstrap.Stderr = os.Stderr
	return bootstrap.Run()
}
