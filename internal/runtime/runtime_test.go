package runtime_test

import (
	"testing"

	"github.com/ConspiracyOS/conos/internal/runtime"
)

func TestBuildStartArgs(t *testing.T) {
	cfg := runtime.ContainerConfig{
		Runtime: "docker",
		Name:    "conos",
		Image:   "conos",
		EnvFile: "srv/dev/container.env",
		SSHPort: 2222,
		Mounts:  []string{"/host/swarm:/Users/vegard/Swarm:ro"},
	}
	args := runtime.BuildStartArgs(cfg)
	if args[0] != "docker" {
		t.Fatalf("expected docker, got %q", args[0])
	}
	if args[1] != "run" || args[2] != "-d" {
		t.Fatalf("unexpected args: %v", args)
	}
	if indexOf(args, "--privileged") == -1 {
		t.Fatalf("expected --privileged in args: %v", args)
	}
	if indexOf(args, "--cgroupns=host") == -1 {
		t.Fatalf("expected --cgroupns=host in args: %v", args)
	}
	envIdx := indexOf(args, "--env-file")
	if envIdx == -1 || args[envIdx+1] != "srv/dev/container.env" {
		t.Fatalf("expected --env-file srv/dev/container.env in args: %v", args)
	}
	// Port mapping
	pIdx := indexOf(args, "-p")
	if pIdx == -1 || args[pIdx+1] != "2222:22" {
		t.Fatalf("expected -p 2222:22 in args: %v", args)
	}
	mountIdx := indexOf(args, "/host/swarm:/Users/vegard/Swarm:ro")
	if mountIdx == -1 || mountIdx == 0 || args[mountIdx-1] != "-v" {
		t.Fatalf("expected bind mount in args: %v", args)
	}
}

func TestBuildStartArgs_NoEnvFile(t *testing.T) {
	cfg := runtime.ContainerConfig{
		Runtime: "container",
		Name:    "cos",
		Image:   "cos",
	}
	args := runtime.BuildStartArgs(cfg)
	for _, a := range args {
		if a == "--env-file" {
			t.Fatal("--env-file should not appear when EnvFile is empty")
		}
		if a == "--privileged" || a == "--cgroupns=host" {
			t.Fatalf("systemd flags should not be used for runtime=container: %v", args)
		}
	}
}

func TestBuildStartArgs_DefaultSSHPortForDocker(t *testing.T) {
	cfg := runtime.ContainerConfig{
		Runtime: "docker",
		Name:    "cos",
		Image:   "cos",
	}

	args := runtime.BuildStartArgs(cfg)
	pIdx := indexOf(args, "-p")
	if pIdx == -1 || args[pIdx+1] != "2222:22" {
		t.Fatalf("expected default -p 2222:22 in args: %v", args)
	}
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}
