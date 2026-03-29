package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes commands on a ConspiracyOS instance.
type Runner interface {
	// Exec runs a command and captures combined output.
	Exec(cmd string) (string, error)
	// Stream runs a command with stdin/stdout/stderr connected to the terminal.
	// Use for interactive commands like logs -f.
	Stream(cmd string) error
}

// SSHRunner implements Runner over SSH.
// All SSH options (user, key, port, ProxyJump) belong in ~/.ssh/config.
type SSHRunner struct {
	Host string
}

func (r *SSHRunner) Exec(cmd string) (string, error) {
	out, err := exec.Command("ssh", r.Host, cmd).CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

func (r *SSHRunner) Stream(cmd string) error {
	c := exec.Command("ssh", r.Host, cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// ContainerRunner implements Runner via docker/podman exec.
type ContainerRunner struct {
	Runtime   string // docker | podman | container
	Container string // container name
}

func (r *ContainerRunner) Exec(cmd string) (string, error) {
	out, err := exec.Command(r.Runtime, "exec", r.Container, "sh", "-c", cmd).CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

func (r *ContainerRunner) Stream(cmd string) error {
	c := exec.Command(r.Runtime, "exec", "-it", r.Container, "sh", "-c", cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// LocalRunner implements Runner for sidecar mode (direct conctl execution).
type LocalRunner struct{}

func (r *LocalRunner) Exec(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

func (r *LocalRunner) Stream(cmd string) error {
	c := exec.Command("sh", "-c", cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// ForTarget returns the appropriate Runner for a target configuration.
func ForTarget(transport, host, runtime, container string) (Runner, error) {
	switch transport {
	case "ssh":
		if host == "" {
			return nil, fmt.Errorf("ssh transport requires host")
		}
		return &SSHRunner{Host: host}, nil
	case "container":
		if runtime == "" {
			runtime = "docker"
		}
		if container == "" {
			return nil, fmt.Errorf("container transport requires container name")
		}
		// Use SSH for commands (container may have its own sshd)
		// but fall back to docker exec if no SSH configured
		if host != "" {
			return &SSHRunner{Host: host}, nil
		}
		return &ContainerRunner{Runtime: runtime, Container: container}, nil
	case "local":
		return &LocalRunner{}, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", transport)
	}
}
