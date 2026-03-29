package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ContainerConfig holds settings for container lifecycle operations.
type ContainerConfig struct {
	Runtime string // docker | podman | container
	Name    string // container name
	Image   string // image name
	EnvFile string // path to env file (empty = omit)
	SSHPort int    // host port to map to container port 22 (default: 2222)
	Mounts  []string
}

func needsSystemdFlags(runtime string) bool {
	return runtime == "docker" || runtime == "podman"
}

func sshPortOrDefault(port int) int {
	if port > 0 {
		return port
	}
	return DefaultSSHPort
}

// BuildStartArgs returns the argument list for the start command.
// Exported for testing.
func BuildStartArgs(cfg ContainerConfig) []string {
	args := []string{cfg.Runtime, "run", "-d", "--name", cfg.Name}
	if needsSystemdFlags(cfg.Runtime) {
		args = append(args,
			"--privileged",
			"--cgroupns=host",
			"-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw",
			"--restart", "unless-stopped",
			"-p", fmt.Sprintf("%d:22", sshPortOrDefault(cfg.SSHPort)),
		)
	}
	for _, mount := range cfg.Mounts {
		if strings.TrimSpace(mount) == "" {
			continue
		}
		args = append(args, "-v", mount)
	}
	if cfg.EnvFile != "" {
		args = append(args, "--env-file", cfg.EnvFile)
	}
	args = append(args, cfg.Image)
	return args
}

// Pull pulls the configured container image.
func Pull(cfg ContainerConfig) error {
	cmd := exec.Command(cfg.Runtime, "pull", cfg.Image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RemoveIfExists force-removes any existing container with the same name.
func RemoveIfExists(cfg ContainerConfig) {
	_ = exec.Command(cfg.Runtime, "rm", "-f", cfg.Name).Run()
}

// Start boots the container.
func Start(cfg ContainerConfig) error {
	args := BuildStartArgs(cfg)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Stop stops and removes the container. If force is false, prompts for confirmation.
func Stop(cfg ContainerConfig, force bool) error {
	if !force {
		fmt.Printf("Stop and remove container %q? [y/N] ", cfg.Name)
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("aborted")
			return nil
		}
	}
	// Stop
	stop := exec.Command(cfg.Runtime, "stop", cfg.Name)
	stop.Stdout = os.Stdout
	stop.Stderr = os.Stderr
	if err := stop.Run(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	// Remove
	rm := exec.Command(cfg.Runtime, "rm", cfg.Name)
	rm.Stdout = os.Stdout
	rm.Stderr = os.Stderr
	return rm.Run()
}
