package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	rt "github.com/ConspiracyOS/conos/internal/runtime"
)

func TestWizardNonInteractive_Defaults(t *testing.T) {
	// Non-TTY, no flags, no env — should return sensible defaults
	t.Setenv("CONOS_API_KEY", "")
	in := strings.NewReader("")
	out := &bytes.Buffer{}
	w := &Wizard{In: in, Out: out, TTY: false}

	opts := w.Run(InstallOpts{})

	if opts.Name != "conos" {
		t.Errorf("expected name 'conos', got %q", opts.Name)
	}
	if opts.Image != defaultImage {
		t.Errorf("expected image %q, got %q", defaultImage, opts.Image)
	}
	if opts.SSHPort != rt.DefaultSSHPort {
		t.Errorf("expected SSH port %d, got %d", rt.DefaultSSHPort, opts.SSHPort)
	}
	if opts.APIKey != "" {
		t.Errorf("expected empty API key, got %q", opts.APIKey)
	}
	// Non-interactive should produce no wizard output
	if out.Len() != 0 {
		t.Errorf("non-interactive wizard should be silent, got: %q", out.String())
	}
}

func TestWizardNonInteractive_EnvKey(t *testing.T) {
	t.Setenv("CONOS_API_KEY", "sk-test-key-12345678")
	in := strings.NewReader("")
	out := &bytes.Buffer{}
	w := &Wizard{In: in, Out: out, TTY: false}

	opts := w.Run(InstallOpts{})

	if opts.APIKey != "sk-test-key-12345678" {
		t.Errorf("expected API key from env, got %q", opts.APIKey)
	}
}

func TestWizardNonInteractive_FlagsOverride(t *testing.T) {
	t.Setenv("CONOS_API_KEY", "")
	in := strings.NewReader("")
	out := &bytes.Buffer{}
	w := &Wizard{In: in, Out: out, TTY: false}

	opts := w.Run(InstallOpts{
		Runtime: "podman",
		Name:    "myconos",
		Image:   "custom:v1",
		SSHPort: 3333,
		APIKey:  "sk-from-flag",
	})

	if opts.Runtime != "podman" {
		t.Errorf("expected runtime 'podman', got %q", opts.Runtime)
	}
	if opts.Name != "myconos" {
		t.Errorf("expected name 'myconos', got %q", opts.Name)
	}
	if opts.Image != "custom:v1" {
		t.Errorf("expected image 'custom:v1', got %q", opts.Image)
	}
	if opts.SSHPort != 3333 {
		t.Errorf("expected SSH port 3333, got %d", opts.SSHPort)
	}
	if opts.APIKey != "sk-from-flag" {
		t.Errorf("expected API key 'sk-from-flag', got %q", opts.APIKey)
	}
}

func TestWizardInteractive_APIKeyPrompt(t *testing.T) {
	t.Setenv("CONOS_API_KEY", "")
	// Simulate user typing an API key
	in := strings.NewReader("sk-interactive-key-9999\n")
	out := &bytes.Buffer{}
	w := &Wizard{In: in, Out: out, TTY: true}

	opts := w.Run(InstallOpts{Runtime: "docker"})

	if opts.APIKey != "sk-interactive-key-9999" {
		t.Errorf("expected interactive API key, got %q", opts.APIKey)
	}
	if !strings.Contains(out.String(), "sk-int...9999") {
		t.Errorf("expected masked key in output, got: %q", out.String())
	}
}

func TestWizardInteractive_APIKeySkip(t *testing.T) {
	t.Setenv("CONOS_API_KEY", "")
	// Simulate user pressing Enter (skip)
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	w := &Wizard{In: in, Out: out, TTY: true}

	opts := w.Run(InstallOpts{Runtime: "docker"})

	if opts.APIKey != "" {
		t.Errorf("expected empty API key on skip, got %q", opts.APIKey)
	}
	if !strings.Contains(out.String(), "Skipped") {
		t.Errorf("expected skip message, got: %q", out.String())
	}
}

func TestWizardInteractive_APIKeyFromEnv(t *testing.T) {
	t.Setenv("CONOS_API_KEY", "sk-env-key-abcd1234")
	in := strings.NewReader("") // no input needed
	out := &bytes.Buffer{}
	w := &Wizard{In: in, Out: out, TTY: true}

	opts := w.Run(InstallOpts{Runtime: "docker"})

	if opts.APIKey != "sk-env-key-abcd1234" {
		t.Errorf("expected env key, got %q", opts.APIKey)
	}
	if !strings.Contains(out.String(), "found in CONOS_API_KEY") {
		t.Errorf("expected env key detection message, got: %q", out.String())
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "****"},
		{"sk-or-v1-abc123def456", "sk-or-...f456"},
		{"", "****"},
	}
	for _, tt := range tests {
		got := maskKey(tt.input)
		if got != tt.want {
			t.Errorf("maskKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectRuntimes(t *testing.T) {
	runtimes := detectRuntimes()
	// Verify each returned runtime is actually findable
	for _, r := range runtimes {
		if _, err := exec.LookPath(r); err != nil {
			t.Errorf("detectRuntimes returned %q but LookPath fails: %v", r, err)
		}
	}
}
