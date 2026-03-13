package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	rt "github.com/ConspiracyOS/conos/internal/runtime"
)

// InstallOpts holds the resolved options for conos install.
type InstallOpts struct {
	Runtime string
	Name    string
	Image   string
	EnvFile string
	SSHPort int
	APIKey  string
}

// Wizard collects install options interactively or from defaults.
type Wizard struct {
	In  io.Reader
	Out io.Writer
	TTY bool // true when stdin is a terminal
}

// NewWizard creates a wizard. If in/out are nil, uses os.Stdin/os.Stdout.
// TTY detection uses os.Stdin by default.
func NewWizard(in io.Reader, out io.Writer) *Wizard {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	tty := false
	if f, ok := in.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			tty = fi.Mode()&os.ModeCharDevice != 0
		}
	}
	return &Wizard{In: in, Out: out, TTY: tty}
}

// Run walks through the install wizard and returns resolved options.
// Flags override wizard prompts. Empty flag values trigger prompts in interactive mode.
func (w *Wizard) Run(flags InstallOpts) InstallOpts {
	opts := flags

	if w.TTY {
		w.printf("\n  ConspiracyOS Install\n\n")
	}

	// 1. Runtime
	if opts.Runtime == "" {
		opts.Runtime = w.pickRuntime()
	} else if w.TTY {
		w.printf("  Runtime: %s\n", opts.Runtime)
	}

	// 2. API key
	if opts.APIKey == "" {
		opts.APIKey = w.askAPIKey()
	} else if w.TTY {
		w.printf("  API key: %s\n", maskKey(opts.APIKey))
	}

	// 3. Container name (default is fine for most users)
	if opts.Name == "" {
		opts.Name = "conos"
	}

	// 4. Image
	if opts.Image == "" {
		opts.Image = defaultImage
	}

	// 5. SSH port
	if opts.SSHPort == 0 {
		opts.SSHPort = rt.DefaultSSHPort
	}

	// 6. Env file
	if opts.EnvFile == "" {
		home := os.Getenv("HOME")
		opts.EnvFile = home + "/.conos/container.env"
	}

	if w.TTY {
		w.printf("\n")
	}

	return opts
}

func (w *Wizard) pickRuntime() string {
	available := detectRuntimes()

	if !w.TTY {
		// Non-interactive: pick first available, default to docker
		if len(available) > 0 {
			return available[0]
		}
		return "docker"
	}

	if len(available) == 0 {
		w.printf("  No container runtime found. Install Docker or Podman first.\n")
		w.printf("  Defaulting to: docker\n")
		return "docker"
	}

	if len(available) == 1 {
		w.printf("  Container runtime: %s (auto-detected)\n", available[0])
		return available[0]
	}

	// Multiple runtimes available — ask
	w.printf("  Available runtimes: %s\n", strings.Join(available, ", "))
	choice := w.prompt("  Which runtime?", available[0])
	for _, r := range available {
		if choice == r {
			return choice
		}
	}
	w.printf("  Unknown runtime %q, using %s\n", choice, available[0])
	return available[0]
}

func (w *Wizard) askAPIKey() string {
	// Check env var first
	key := strings.TrimSpace(os.Getenv("CONOS_API_KEY"))
	if key != "" {
		if w.TTY {
			w.printf("  API key: found in CONOS_API_KEY (%s)\n", maskKey(key))
		}
		return key
	}

	if !w.TTY {
		return "" // non-interactive, no key — install proceeds without it
	}

	w.printf("\n  ConspiracyOS agents need an LLM API key to think.\n")
	w.printf("  Supported providers: OpenRouter, Anthropic, OpenAI\n")
	w.printf("  You can add this later in ~/.conos/container.env\n\n")
	key = w.prompt("  API key (or press Enter to skip)", "")
	if key != "" {
		w.printf("  Got it (%s)\n", maskKey(key))
	} else {
		w.printf("  Skipped. Add it later:\n")
		w.printf("    echo 'CONOS_API_KEY=sk-your-key' >> ~/.conos/container.env\n")
		w.printf("    conos stop --force && conos start\n")
	}
	return key
}

// prompt shows a question with a default value and reads a line.
func (w *Wizard) prompt(question string, defaultVal string) string {
	if defaultVal != "" {
		w.printf("%s [%s]: ", question, defaultVal)
	} else {
		w.printf("%s: ", question)
	}
	scanner := bufio.NewScanner(w.In)
	if scanner.Scan() {
		answer := strings.TrimSpace(scanner.Text())
		if answer == "" {
			return defaultVal
		}
		return answer
	}
	return defaultVal
}

func (w *Wizard) printf(format string, args ...any) {
	fmt.Fprintf(w.Out, format, args...)
}

// detectRuntimes returns available container runtimes on this system.
func detectRuntimes() []string {
	var found []string
	for _, name := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(name); err == nil {
			found = append(found, name)
		}
	}
	return found
}

// maskKey shows first 6 and last 4 chars, masks the rest.
func maskKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:6] + "..." + key[len(key)-4:]
}
