package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if cfg.Server != "" || cfg.SSHKey != "" || cfg.TLSCA != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	want := Config{
		Server: "https://secrets.example.com",
		SSHKey: "/home/user/.ssh/id_ed25519",
		TLSCA:  "/etc/ssl/custom-ca.pem",
	}

	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	// Verify the file was written.
	path := filepath.Join(dir, "dbx", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not found at %s: %v", path, err)
	}

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if got != want {
		t.Errorf("round-trip failed:\n  got  %+v\n  want %+v", got, want)
	}
}

func TestConfigServerURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := Config{Server: "https://my-server.dev:8443"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server != "https://my-server.dev:8443" {
		t.Errorf("Server = %q, want https://my-server.dev:8443", loaded.Server)
	}
}

func TestConfigFilePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := Config{Server: "https://example.com"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "dbx", "config.toml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("config file permissions = %04o, want 0600", perm)
	}
}

func TestConfigCustomCA(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := Config{TLSCA: "/opt/certs/ca-bundle.crt"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TLSCA != "/opt/certs/ca-bundle.crt" {
		t.Errorf("TLSCA = %q, want /opt/certs/ca-bundle.crt", loaded.TLSCA)
	}
}
