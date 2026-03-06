package runner

import (
	"os"
	"os/exec"
	"strings"
)

// Runner executes commands on the ConspiracyOS instance.
type Runner interface {
	// Exec runs a command and captures combined output.
	Exec(cmd string) (string, error)
	// Stream runs a command with stdin/stdout/stderr connected to the terminal.
	// Use for interactive commands like logs -f.
	Stream(cmd string) error
}

// SSHRunner implements Runner over SSH.
// It runs: ssh <Host> <cmd>
// All SSH options (user, key, port, ProxyJump) belong in ~/.ssh/config.
type SSHRunner struct {
	Host string
}

// Exec runs cmd on the remote host and returns combined output.
func (r *SSHRunner) Exec(cmd string) (string, error) {
	out, err := exec.Command("ssh", r.Host, cmd).CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

// Stream runs cmd on the remote host with terminal I/O connected directly.
func (r *SSHRunner) Stream(cmd string) error {
	c := exec.Command("ssh", r.Host, cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
