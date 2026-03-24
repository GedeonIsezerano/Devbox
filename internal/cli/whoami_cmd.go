package cli

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// WhoamiResult holds the whoami output fields.
type WhoamiResult struct {
	Server      string `json:"server"`
	AuthMethod  string `json:"auth_method"`
	Fingerprint string `json:"fingerprint,omitempty"`
	KeyFile     string `json:"key_file,omitempty"`
	TokenPrefix string `json:"token_prefix,omitempty"`
}

// RunWhoami performs `dbx whoami`.
func RunWhoami(printer *Printer) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	result := WhoamiResult{
		Server: cfg.Server,
	}

	if cfg.Server == "" {
		result.Server = "(not configured)"
	}

	if token := os.Getenv("DEVBOX_TOKEN"); token != "" {
		result.AuthMethod = "token"
		if len(token) > 12 {
			result.TokenPrefix = token[:12] + "..."
		} else {
			result.TokenPrefix = token
		}
	} else {
		candidate, keyErr := discoverSSHKey(cfg.SSHKey)
		if keyErr != nil {
			result.AuthMethod = "none"
		} else {
			result.Fingerprint = ssh.FingerprintSHA256(candidate.PublicKey)
			if candidate.FromAgent {
				result.AuthMethod = "ssh-agent"
			} else {
				result.AuthMethod = "ssh-key"
				result.KeyFile = candidate.Path
			}
		}
	}

	if printer.JSON {
		return printer.Data(result)
	}

	printer.Info("Server:      %s", result.Server)
	printer.Info("Auth method: %s", result.AuthMethod)
	if result.Fingerprint != "" {
		printer.Info("Fingerprint: %s", result.Fingerprint)
	}
	if result.KeyFile != "" {
		printer.Info("Key file:    %s", result.KeyFile)
	}
	if result.TokenPrefix != "" {
		printer.Info("Token:       %s", result.TokenPrefix)
	}

	return nil
}
