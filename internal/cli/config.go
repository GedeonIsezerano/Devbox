package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds CLI configuration values persisted in config.toml.
type Config struct {
	Server string `toml:"server"`
	SSHKey string `toml:"ssh_key"`
	TLSCA  string `toml:"tls_ca"`
}

// ConfigPath returns the path to the configuration file,
// typically ~/.config/dbx/config.toml. It respects $XDG_CONFIG_HOME.
func ConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dbx", "config.toml")
}

// LoadConfig reads the configuration file. If the file does not exist an
// empty Config is returned with no error.
func LoadConfig() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// SaveConfig writes the configuration to disk, creating parent directories
// as needed.
func SaveConfig(cfg Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}
