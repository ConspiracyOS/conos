package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rt "github.com/ConspiracyOS/conos/internal/runtime"
)

func TestWriteEnvFile_WithKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "container.env")

	writeEnvFile(path, "sk-test-key-123")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("env file not created: %v", err)
	}
	if string(data) != "CONOS_API_KEY=sk-test-key-123\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
	// Verify permissions
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %o", info.Mode().Perm())
	}
}

func TestWriteEnvFile_WithoutKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "container.env")

	writeEnvFile(path, "")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("env file not created: %v", err)
	}
	if !strings.Contains(string(data), "# CONOS_API_KEY=") {
		t.Errorf("expected commented key placeholder, got: %q", string(data))
	}
}

func TestWriteEnvFile_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "container.env")

	// Write existing content
	os.WriteFile(path, []byte("EXISTING=value\n"), 0o600)

	writeEnvFile(path, "sk-new-key")

	data, _ := os.ReadFile(path)
	if string(data) != "EXISTING=value\n" {
		t.Errorf("existing file was overwritten: %q", string(data))
	}
}

func TestWriteEnvFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "container.env")

	writeEnvFile(path, "sk-key")

	if _, err := os.Stat(path); err != nil {
		t.Errorf("nested file not created: %v", err)
	}
}

func TestGenerateConfig_CreatesConfigAndSSH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create .ssh dir (generateConfig -> ensureSSHConfig needs it)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)

	cfg := rt.ContainerConfig{
		Runtime: "docker",
		Name:    "conos",
		Image:   "ghcr.io/conspiracyos/conos:latest",
		EnvFile: filepath.Join(home, ".conos", "container.env"),
		SSHPort: 2222,
	}

	err := generateConfig(cfg)
	if err != nil {
		t.Fatalf("generateConfig failed: %v", err)
	}

	// Check conos.toml was created
	configPath := filepath.Join(home, ".conos", "conos.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `host = "conos"`) {
		t.Errorf("config missing host, got: %s", content)
	}
	if !strings.Contains(content, `runtime = "docker"`) {
		t.Errorf("config missing runtime, got: %s", content)
	}

	// Check SSH config was appended
	sshData, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		t.Fatalf("SSH config not created: %v", err)
	}
	sshContent := string(sshData)
	if !strings.Contains(sshContent, "Host conos") {
		t.Errorf("SSH config missing Host entry, got: %s", sshContent)
	}
	if !strings.Contains(sshContent, "Port 2222") {
		t.Errorf("SSH config missing port, got: %s", sshContent)
	}
}

func TestGenerateConfig_DoesNotOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".conos")
	os.MkdirAll(configDir, 0o700)
	os.WriteFile(filepath.Join(configDir, "conos.toml"), []byte("existing config\n"), 0o600)

	cfg := rt.ContainerConfig{
		Runtime: "docker",
		Name:    "conos",
		Image:   "test:latest",
		SSHPort: 2222,
	}

	err := generateConfig(cfg)
	if err != nil {
		t.Fatalf("generateConfig failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(configDir, "conos.toml"))
	if string(data) != "existing config\n" {
		t.Errorf("config was overwritten: %q", string(data))
	}
}

func TestEnsureSSHConfig_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)

	cfg := rt.ContainerConfig{Name: "conos", SSHPort: 2222}

	// First call
	ensureSSHConfig(cfg)
	// Second call
	ensureSSHConfig(cfg)

	data, _ := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	count := strings.Count(string(data), "Host conos")
	if count != 1 {
		t.Errorf("expected 1 Host entry, got %d", count)
	}
}

func TestEnsureSSHConfig_CustomPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)

	cfg := rt.ContainerConfig{Name: "myconos", SSHPort: 3333}
	ensureSSHConfig(cfg)

	data, _ := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	content := string(data)
	if !strings.Contains(content, "Host myconos") {
		t.Errorf("missing custom host name, got: %s", content)
	}
	if !strings.Contains(content, "Port 3333") {
		t.Errorf("missing custom port, got: %s", content)
	}
}

func TestEnsureSSHConfig_PreservesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)

	// Write existing SSH config
	existing := "Host myserver\n  HostName 10.0.0.1\n  User admin\n"
	os.WriteFile(filepath.Join(sshDir, "config"), []byte(existing), 0o600)

	cfg := rt.ContainerConfig{Name: "conos", SSHPort: 2222}
	ensureSSHConfig(cfg)

	data, _ := os.ReadFile(filepath.Join(sshDir, "config"))
	content := string(data)
	if !strings.Contains(content, "Host myserver") {
		t.Errorf("existing config was lost")
	}
	if !strings.Contains(content, "Host conos") {
		t.Errorf("new entry not appended")
	}
}
