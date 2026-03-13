package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultSSHPort is the host port mapped to container SSH.
const DefaultSSHPort = 2222

// InjectSSHKey copies the user's SSH public key into the container's
// authorized_keys for root. It waits for sshd to be ready.
func InjectSSHKey(cfg ContainerConfig) error {
	pubKey, err := findSSHPubKey()
	if err != nil {
		return fmt.Errorf("SSH key injection: %w", err)
	}

	// Wait for container to be ready (systemd boot takes a moment)
	if err := waitForContainer(cfg, 30*time.Second); err != nil {
		return fmt.Errorf("container not ready: %w", err)
	}

	// Inject the key
	cmd := exec.Command(cfg.Runtime, "exec", cfg.Name, "bash", "-c",
		fmt.Sprintf("mkdir -p /root/.ssh && chmod 700 /root/.ssh && echo %q >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys",
			strings.TrimSpace(pubKey)))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("injecting SSH key: %w", err)
	}
	return nil
}

// findSSHPubKey looks for the user's SSH public key in standard locations.
func findSSHPubKey() (string, error) {
	home := os.Getenv("HOME")
	candidates := []string{
		filepath.Join(home, ".ssh", "id_ed25519.pub"),
		filepath.Join(home, ".ssh", "id_rsa.pub"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("no SSH public key found in ~/.ssh/ (tried id_ed25519.pub, id_rsa.pub)")
}

// waitForContainer polls until the container is running.
func waitForContainer(cfg ContainerConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command(cfg.Runtime, "exec", cfg.Name, "echo", "ready")
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out after %s", timeout)
}
