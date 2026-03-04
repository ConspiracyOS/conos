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
	}
	args := runtime.BuildStartArgs(cfg)
	// Expected: docker run -d --name conos --env-file srv/dev/container.env conos
	if args[0] != "docker" {
		t.Fatalf("expected docker, got %q", args[0])
	}
	if args[1] != "run" || args[2] != "-d" {
		t.Fatalf("unexpected args: %v", args)
	}
	envIdx := indexOf(args, "--env-file")
	if envIdx == -1 || args[envIdx+1] != "srv/dev/container.env" {
		t.Fatalf("expected --env-file srv/dev/container.env in args: %v", args)
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
