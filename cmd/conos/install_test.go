package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEnvFile_WithKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "container.env")

	writeEnvFile(path, "sk-test-key-123")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("env file not created: %v", err)
	}
	if string(data) != "CONOS_API_KEY=sk-test-key-123\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %o", info.Mode().Perm())
	}
}

func TestWriteEnvFile_WithoutKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
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
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "container.env")

	os.WriteFile(path, []byte("EXISTING=value\n"), 0o600)
	writeEnvFile(path, "sk-new-key")

	data, _ := os.ReadFile(path)
	if string(data) != "EXISTING=value\n" {
		t.Errorf("existing file was overwritten: %q", string(data))
	}
}

func TestEnsureSSHConfig_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)

	ensureSSHConfig("conos", 2222)
	ensureSSHConfig("conos", 2222)

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

	ensureSSHConfig("myconos", 3333)

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

	existing := "Host myserver\n  HostName 10.0.0.1\n  User admin\n"
	os.WriteFile(filepath.Join(sshDir, "config"), []byte(existing), 0o600)

	ensureSSHConfig("conos", 2222)

	data, _ := os.ReadFile(filepath.Join(sshDir, "config"))
	content := string(data)
	if !strings.Contains(content, "Host myserver") {
		t.Errorf("existing config was lost")
	}
	if !strings.Contains(content, "Host conos") {
		t.Errorf("new entry not appended")
	}
}
