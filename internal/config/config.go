package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const defaultImage = "ghcr.io/conspiracyos/conos:latest"
const defaultSSHPort = 2222

// Target represents a single ConOS instance that conos can manage.
type Target struct {
	Name      string   `toml:"name"`
	Transport string   `toml:"transport"` // container | ssh | local
	Default   bool     `toml:"default"`
	Runtime   string   `toml:"runtime"`   // docker | podman | container (for transport=container)
	Container string   `toml:"container"` // container name (for transport=container)
	Host      string   `toml:"host"`      // SSH host alias (for transport=ssh and container)
	Image     string   `toml:"image"`
	EnvFile   string   `toml:"env_file"`
	SSHPort   int      `toml:"ssh_port"`
	Mounts    []string `toml:"mounts"`
}

// Config is the conos config loaded from ~/.conos/conos.toml.
type Config struct {
	Targets []Target `toml:"targets"`

	// Legacy single-instance fields (auto-migrated to targets)
	Instance  Instance  `toml:"instance"`
	Container LegacyCtr `toml:"container"`
}

// Instance holds legacy SSH connection settings.
type Instance struct {
	Host string `toml:"host"`
}

// LegacyCtr holds legacy container runtime settings.
type LegacyCtr struct {
	Runtime string   `toml:"runtime"`
	Name    string   `toml:"name"`
	Image   string   `toml:"image"`
	EnvFile string   `toml:"env_file"`
	SSHPort int      `toml:"ssh_port"`
	Mounts  []string `toml:"mounts"`
}

func searchPaths() []string {
	return []string{
		".conos/conos.toml",
		filepath.Join(os.Getenv("HOME"), ".conos", "conos.toml"),
	}
}

// Load finds and loads the config file.
func Load() (*Config, error) {
	for _, p := range searchPaths() {
		if _, err := os.Stat(p); err == nil {
			return LoadFile(p)
		}
	}
	return nil, fmt.Errorf("no config found; run `conos install` to create one")
}

// LoadFile loads config from an explicit path.
func LoadFile(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}

	// Migrate legacy format: [instance] + [container] -> single target
	if len(cfg.Targets) == 0 && cfg.Instance.Host != "" {
		name := cfg.Container.Name
		if name == "" {
			name = "conos"
		}
		t := Target{
			Name:      name,
			Transport: "container",
			Default:   true,
			Runtime:   cfg.Container.Runtime,
			Container: name,
			Host:      cfg.Instance.Host,
			Image:     cfg.Container.Image,
			EnvFile:   cfg.Container.EnvFile,
			SSHPort:   cfg.Container.SSHPort,
			Mounts:    cfg.Container.Mounts,
		}
		if t.Runtime == "" {
			t.Runtime = "docker"
		}
		if t.Image == "" {
			t.Image = defaultImage
		}
		cfg.Targets = []Target{t}
	}

	// Apply defaults
	for i := range cfg.Targets {
		if cfg.Targets[i].Transport == "" {
			cfg.Targets[i].Transport = "container"
		}
		if cfg.Targets[i].Transport == "container" {
			if cfg.Targets[i].Runtime == "" {
				cfg.Targets[i].Runtime = "docker"
			}
			if cfg.Targets[i].Container == "" {
				cfg.Targets[i].Container = cfg.Targets[i].Name
			}
			if cfg.Targets[i].Image == "" {
				cfg.Targets[i].Image = defaultImage
			}
			if cfg.Targets[i].SSHPort == 0 {
				cfg.Targets[i].SSHPort = defaultSSHPort
			}
			if cfg.Targets[i].Host == "" {
				cfg.Targets[i].Host = cfg.Targets[i].Name
			}
		}
	}

	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("no targets defined in %s; run `conos install` to add one", path)
	}

	return &cfg, nil
}

// DefaultTarget returns the default target.
func (c *Config) DefaultTarget() (*Target, error) {
	for i := range c.Targets {
		if c.Targets[i].Default {
			return &c.Targets[i], nil
		}
	}
	// If only one target, it's the default
	if len(c.Targets) == 1 {
		return &c.Targets[0], nil
	}
	return nil, fmt.Errorf("no default target; use `conos <target> <command>` or set default = true")
}

// FindTarget returns a target by name.
func (c *Config) FindTarget(name string) (*Target, bool) {
	for i := range c.Targets {
		if c.Targets[i].Name == name {
			return &c.Targets[i], true
		}
	}
	return nil, false
}

// TargetNames returns all target names.
func (c *Config) TargetNames() []string {
	names := make([]string, len(c.Targets))
	for i, t := range c.Targets {
		names[i] = t.Name
	}
	return names
}

// ConfigPath returns the path to the config file, creating ~/.conos/ if needed.
func ConfigPath() string {
	home := os.Getenv("HOME")
	return filepath.Join(home, ".conos", "conos.toml")
}

// AddTarget appends a target to the config file. If the file doesn't exist, creates it.
func AddTarget(t Target) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	// Load existing or start fresh
	var cfg Config
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return fmt.Errorf("loading existing config: %w", err)
		}
	}

	// Check for duplicate
	for _, existing := range cfg.Targets {
		if existing.Name == t.Name {
			return fmt.Errorf("target %q already exists", t.Name)
		}
	}

	// If this is the first target, make it default
	if len(cfg.Targets) == 0 {
		t.Default = true
	}

	cfg.Targets = append(cfg.Targets, t)

	// Write back — only the [[targets]] section
	return writeTargets(path, cfg.Targets)
}

func writeTargets(path string, targets []Target) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	// Wrap in a struct so TOML encodes as [[targets]]
	return enc.Encode(struct {
		Targets []Target `toml:"targets"`
	}{Targets: targets})
}
