package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ConspiracyOS/conos/internal/config"
)

func TestLoad_MinimalConfig(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`[instance]
host = "mybox"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Instance.Host != "mybox" {
		t.Fatalf("expected host mybox, got %q", cfg.Instance.Host)
	}
	// Container defaults
	if cfg.Container.Runtime != "docker" {
		t.Fatalf("expected runtime docker, got %q", cfg.Container.Runtime)
	}
	if cfg.Container.Name != "conos" {
		t.Fatalf("expected name conos, got %q", cfg.Container.Name)
	}
	if cfg.Container.Image != "ghcr.io/conspiracyos/conos:latest" {
		t.Fatalf("unexpected default image: %q", cfg.Container.Image)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`[instance]
host = "box"

[container]
runtime = "container"
name = "cos"
image = "cos"
env_file = "srv/prod/container.env"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Container.Runtime != "container" {
		t.Fatalf("expected container, got %q", cfg.Container.Runtime)
	}
	if cfg.Container.EnvFile != "srv/prod/container.env" {
		t.Fatalf("unexpected env_file: %q", cfg.Container.EnvFile)
	}
}

func TestLoad_MissingHost(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`[instance]
`), 0644)
	_, err := config.LoadFile(f)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestLoadContainer_DefaultsWithoutConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	container, err := config.LoadContainer()
	if err != nil {
		t.Fatal(err)
	}
	if container.Runtime != "docker" || container.Name != "conos" || container.Image != "ghcr.io/conspiracyos/conos:latest" {
		t.Fatalf("unexpected defaults: %+v", container)
	}
}

func TestLoadContainer_AllowsMissingHost(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".conos"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".conos", "conos.toml"), []byte(`[container]
runtime = "docker"
name = "my-conos"
image = "ghcr.io/conspiracyos/conos:latest"
`), 0644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	container, err := config.LoadContainer()
	if err != nil {
		t.Fatal(err)
	}
	if container.Name != "my-conos" {
		t.Fatalf("unexpected container name: %q", container.Name)
	}
}
