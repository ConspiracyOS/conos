package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the conos project config loaded from .conos/conos.toml.
type Config struct {
	Instance  Instance  `toml:"instance"`
	Container Container `toml:"container"`
}

// Instance holds SSH connection settings.
type Instance struct {
	Host string `toml:"host"` // SSH host alias from ~/.ssh/config
}

// Container holds container runtime settings for start/stop.
type Container struct {
	Runtime string `toml:"runtime"`  // docker | podman | container (default: docker)
	Name    string `toml:"name"`     // container name (default: conos)
	Image   string `toml:"image"`    // image name (default: conos)
	EnvFile string `toml:"env_file"` // path to env file (optional)
}

// Load finds and loads the config file. Searches .conos/conos.toml then ~/.conos/conos.toml.
func Load() (*Config, error) {
	paths := []string{
		".conos/conos.toml",
		filepath.Join(os.Getenv("HOME"), ".conos", "conos.toml"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return LoadFile(p)
		}
	}
	return nil, fmt.Errorf("no config found; create .conos/conos.toml with:\n\n[instance]\nhost = \"<ssh-host-alias>\"\n")
}

// LoadFile loads config from an explicit path.
func LoadFile(path string) (*Config, error) {
	cfg := &Config{
		Container: Container{
			Runtime: "docker",
			Name:    "conos",
			Image:   "conos",
		},
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	if cfg.Instance.Host == "" {
		return nil, fmt.Errorf("instance.host is required in %s", path)
	}
	return cfg, nil
}
