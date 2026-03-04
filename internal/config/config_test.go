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
