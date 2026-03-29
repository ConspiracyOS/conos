package commands_test

import (
	"errors"
	"testing"

	"github.com/ConspiracyOS/conos/internal/commands"
)

type mockRunner struct {
	cmd string
	out string
	err error
}

func (m *mockRunner) Exec(cmd string) (string, error) {
	m.cmd = cmd
	return m.out, m.err
}
func (m *mockRunner) Stream(cmd string) error {
	m.cmd = cmd
	return m.err
}

func TestStatus_CallsConctlStatus(t *testing.T) {
	r := &mockRunner{out: "concierge  active  (0 pending)"}
	out, err := commands.Status(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.cmd != "conctl status" {
		t.Fatalf("expected 'conctl status', got %q", r.cmd)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestStatus_PropagatesError(t *testing.T) {
	r := &mockRunner{err: errors.New("ssh failed")}
	_, err := commands.Status(r)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAgentList_CallsConctlStatus(t *testing.T) {
	r := &mockRunner{out: "researcher  inactive  (2 pending)"}
	out, err := commands.AgentList(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.cmd != "conctl status" {
		t.Fatalf("expected 'conctl status', got %q", r.cmd)
	}
	_ = out
}

func TestAgentSend_CallsConctlTask(t *testing.T) {
	r := &mockRunner{}
	err := commands.AgentSend(r, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if r.cmd != "conctl task 'hello world'" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestAgentSendTo_CallsConctlTaskWithAgent(t *testing.T) {
	r := &mockRunner{}
	err := commands.AgentSendTo(r, "sysadmin", "fix the logs")
	if err != nil {
		t.Fatal(err)
	}
	if r.cmd != "conctl task --agent sysadmin 'fix the logs'" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestAgentSend_EscapesSingleQuotes(t *testing.T) {
	r := &mockRunner{}
	commands.AgentSend(r, "it's a test")
	if r.cmd != "conctl task 'it'\\''s a test'" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestAgentLogs_Default(t *testing.T) {
	r := &mockRunner{}
	commands.AgentLogs(r, false, 20, "")
	if r.cmd != "conctl logs" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestAgentLogs_WithFlags(t *testing.T) {
	r := &mockRunner{}
	commands.AgentLogs(r, false, 50, "sysadmin")
	if r.cmd != "conctl logs -n 50 sysadmin" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestAgentLogs_Follow(t *testing.T) {
	r := &mockRunner{}
	commands.AgentLogs(r, true, 20, "")
	if r.cmd != "conctl logs -f -n 20" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestAgentKill(t *testing.T) {
	r := &mockRunner{}
	commands.AgentKill(r, "researcher")
	if r.cmd != "conctl kill researcher" {
		t.Fatalf("unexpected cmd: %q", r.cmd)
	}
}

func TestBuildScpArgs(t *testing.T) {
	args := commands.BuildScpArgs("mybox", ".conos/conos.toml")
	if args[0] != "scp" {
		t.Fatalf("expected scp, got %q", args[0])
	}
	found := false
	for _, a := range args {
		if a == ".conos/conos.toml" {
			found = true
		}
	}
	if !found {
		t.Fatalf("local path not in scp args: %v", args)
	}
	var remote string
	for _, a := range args {
		if len(a) > len("mybox:") && a[:len("mybox:")] == "mybox:" {
			remote = a
		}
	}
	if remote != "mybox:/etc/conos/conos.toml" {
		t.Fatalf("unexpected remote: %q (args: %v)", remote, args)
	}
}

func TestBuildConfigFixupCmd(t *testing.T) {
	cmd := commands.BuildConfigFixupCmd()
	if cmd != "chown root:agents /etc/conos/conos.toml && chmod 640 /etc/conos/conos.toml" {
		t.Fatalf("unexpected fixup cmd: %q", cmd)
	}
}
