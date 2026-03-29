package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ConspiracyOS/conos/internal/config"
)

func TestLoadFile_LegacyFormat(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`[instance]
host = "mybox"

[container]
runtime = "docker"
name = "conos"
image = "ghcr.io/conspiracyos/conos:latest"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target from legacy config, got %d", len(cfg.Targets))
	}
	tgt := cfg.Targets[0]
	if tgt.Name != "conos" {
		t.Errorf("expected name conos, got %q", tgt.Name)
	}
	if tgt.Transport != "container" {
		t.Errorf("expected transport container, got %q", tgt.Transport)
	}
	if tgt.Host != "mybox" {
		t.Errorf("expected host mybox, got %q", tgt.Host)
	}
	if !tgt.Default {
		t.Error("legacy target should be default")
	}
}

func TestLoadFile_TargetFormat(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`
[[targets]]
name = "cos"
transport = "container"
runtime = "docker"
container = "cos"
host = "cos"
default = true

[[targets]]
name = "devvps"
transport = "ssh"
host = "devvps"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(cfg.Targets))
	}

	def, err := cfg.DefaultTarget()
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "cos" {
		t.Errorf("expected default cos, got %q", def.Name)
	}

	vps, found := cfg.FindTarget("devvps")
	if !found {
		t.Fatal("devvps target not found")
	}
	if vps.Transport != "ssh" {
		t.Errorf("expected ssh transport, got %q", vps.Transport)
	}
}

func TestLoadFile_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`
[[targets]]
name = "dev"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	tgt := cfg.Targets[0]
	if tgt.Transport != "container" {
		t.Errorf("expected default transport container, got %q", tgt.Transport)
	}
	if tgt.Runtime != "docker" {
		t.Errorf("expected default runtime docker, got %q", tgt.Runtime)
	}
	if tgt.Container != "dev" {
		t.Errorf("expected container name to default to target name, got %q", tgt.Container)
	}
	if tgt.SSHPort != 2222 {
		t.Errorf("expected default SSH port 2222, got %d", tgt.SSHPort)
	}
}

func TestDefaultTarget_SingleTarget(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`
[[targets]]
name = "only"
transport = "local"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	def, err := cfg.DefaultTarget()
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "only" {
		t.Errorf("single target should be default, got %q", def.Name)
	}
}

func TestDefaultTarget_NoDefault(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "conos.toml")
	os.WriteFile(f, []byte(`
[[targets]]
name = "a"
transport = "local"

[[targets]]
name = "b"
transport = "ssh"
host = "b"
`), 0644)

	cfg, err := config.LoadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.DefaultTarget()
	if err == nil {
		t.Error("expected error when multiple targets and no default")
	}
}

func TestAddTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := config.AddTarget(config.Target{
		Name:      "first",
		Transport: "ssh",
		Host:      "first-host",
	})
	if err != nil {
		t.Fatal(err)
	}

	// First target should be auto-default
	cfg, err := config.LoadFile(filepath.Join(home, ".conos", "conos.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	if !cfg.Targets[0].Default {
		t.Error("first target should be default")
	}

	// Add second
	err = config.AddTarget(config.Target{
		Name:      "second",
		Transport: "local",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err = config.LoadFile(filepath.Join(home, ".conos", "conos.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(cfg.Targets))
	}

	// Duplicate should error
	err = config.AddTarget(config.Target{Name: "first", Transport: "local"})
	if err == nil {
		t.Error("expected error for duplicate target name")
	}
}
