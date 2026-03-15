package driverutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// SSHConfig holds SSH connection parameters common to all drivers.
type SSHConfig struct {
	Host string
	Port string
	User string
	Key  string
}

// DefaultSSHConfig returns an SSHConfig with standard defaults applied.
// Empty fields are set to: Host=localhost, Port=22, User=root, Key=$HOME/.ssh/id_ed25519.
func DefaultSSHConfig(host, port, user, key string) SSHConfig {
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "22"
	}
	if user == "" {
		user = "root"
	}
	if key == "" {
		key = "$HOME/.ssh/id_ed25519"
	}
	return SSHConfig{Host: host, Port: port, User: user, Key: key}
}

// Executor abstracts command execution against a ConspiracyOS instance.
type Executor interface {
	Run(cmd string) (string, error)
}

// SSHExecutor runs commands via SSH.
type SSHExecutor struct {
	Config SSHConfig
}

// Run executes cmd on the remote host and returns combined output.
func (e *SSHExecutor) Run(cmd string) (string, error) {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-i", e.Config.Key,
		"-p", e.Config.Port,
		fmt.Sprintf("%s@%s", e.Config.User, e.Config.Host),
		cmd,
	}
	out, err := exec.Command("ssh", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
