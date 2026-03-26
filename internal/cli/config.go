package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

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

// SessionPath returns the path to the session token file,
// typically ~/.config/dbx/session.
func SessionPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dbx", "session")
}

// LoadSession reads a cached session token from disk. Returns an empty string
// and nil error if the file does not exist.
func LoadSession() (string, error) {
	data, err := os.ReadFile(SessionPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SaveSession writes a session token to disk with 0600 permissions.
func SaveSession(token string) error {
	path := SessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// ClearSession removes the cached session token file.
func ClearSession() error {
	err := os.Remove(SessionPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
