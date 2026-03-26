package cli

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/user/devbox/internal/crypto"
)

// sshKeyCandidate holds the path and parsed key material for a discovered SSH key.
type sshKeyCandidate struct {
	PublicKey   ssh.PublicKey
	Signer     ssh.Signer
	Path       string   // path to the key file (empty for agent keys)
	Comment    string   // key comment from authorized_keys format
	FromAgent  bool
	agentConn  net.Conn // kept open for agent-based signers; caller must close
}

// ResolveAuth creates an authenticated client using the auth resolution order:
//  1. DEVBOX_TOKEN env var
//  2. SSH key challenge-response
//
// Returns a Client with the session token set.
func ResolveAuth(printer *Printer) (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if cfg.Server == "" {
		return nil, fmt.Errorf("no server configured — run `dbx auth login` first")
	}

	// 1. Check DEVBOX_TOKEN env var.
	if token := os.Getenv("DEVBOX_TOKEN"); token != "" {
		client, err := NewClient(cfg.Server, "", cfg.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("creating client: %w", err)
		}

		resp, err := client.TokenAuth(token)
		if err != nil {
			return nil, fmt.Errorf("token authentication failed: %w", err)
		}

		client.AuthToken = resp.SessionToken
		printer.Info("Authenticated via DEVBOX_TOKEN")
		return client, nil
	}

	// 2. SSH key challenge-response.
	candidate, err := discoverSSHKey(cfg.SSHKey)
	if err != nil {
		return nil, fmt.Errorf("no SSH key found: %w", err)
	}
	if candidate.agentConn != nil {
		defer candidate.agentConn.Close()
	}

	fingerprint := ssh.FingerprintSHA256(candidate.PublicKey)

	client, err := NewClient(cfg.Server, "", cfg.TLSCA)
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}

	// Challenge-response flow.
	challengeResp, err := client.Challenge()
	if err != nil {
		return nil, fmt.Errorf("requesting challenge: %w", err)
	}

	nonceBytes, err := hex.DecodeString(challengeResp.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decoding nonce: %w", err)
	}

	sig, err := crypto.SignSSH(candidate.Signer, nonceBytes)
	if err != nil {
		return nil, fmt.Errorf("signing challenge: %w", err)
	}

	verifyResp, err := client.Verify(fingerprint, sig, challengeResp.Nonce)
	if err != nil {
		return nil, fmt.Errorf("verification failed: %w", err)
	}

	client.AuthToken = verifyResp.SessionToken
	if candidate.FromAgent {
		printer.Info("Authenticated via ssh-agent")
	} else {
		printer.Info("Authenticated via SSH key %s", candidate.Path)
	}
	return client, nil
}

// discoverSSHKey finds an SSH key using the following resolution order:
//  1. If preferredPath is set, try that file first.
//  2. Try ssh-agent via SSH_AUTH_SOCK.
//  3. Try common key files: id_ed25519, id_ed25519_sk, id_ecdsa, id_rsa.
func discoverSSHKey(preferredPath string) (*sshKeyCandidate, error) {
	// If a specific key is configured, use it exclusively.
	if preferredPath != "" {
		candidate, err := loadKeyFromFile(preferredPath)
		if err != nil {
			return nil, fmt.Errorf("loading configured key %s: %w", preferredPath, err)
		}
		return candidate, nil
	}

	// Try ssh-agent.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		candidate, err := trySSHAgent(sock)
		if err == nil {
			return candidate, nil
		}
		// Fall through to file-based keys.
	}

	// Try common key files.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}

	keyNames := []string{"id_ed25519", "id_ed25519_sk", "id_ecdsa", "id_rsa"}
	for _, name := range keyNames {
		path := filepath.Join(home, ".ssh", name)
		candidate, err := loadKeyFromFile(path)
		if err == nil {
			return candidate, nil
		}
	}

	return nil, fmt.Errorf("no SSH key found in agent or ~/.ssh/")
}

// trySSHAgent connects to the SSH agent and returns the first available key.
// The caller must close candidate.agentConn when done with the signer.
func trySSHAgent(sock string) (*sshKeyCandidate, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("connecting to ssh-agent: %w", err)
	}

	agentClient := agent.NewClient(conn)
	signers, err := agentClient.Signers()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("listing agent keys: %w", err)
	}

	if len(signers) == 0 {
		conn.Close()
		return nil, fmt.Errorf("no keys in ssh-agent")
	}

	// Use the first key from the agent.
	// Connection must stay open — the signer signs via the agent protocol.
	signer := signers[0]
	return &sshKeyCandidate{
		PublicKey: signer.PublicKey(),
		Signer:   signer,
		FromAgent: true,
		Comment:  "ssh-agent key",
		agentConn: conn,
	}, nil
}

// loadKeyFromFile reads a private key file and its .pub companion.
func loadKeyFromFile(path string) (*sshKeyCandidate, error) {
	privData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(privData)
	if err != nil {
		return nil, fmt.Errorf("parsing private key %s: %w", path, err)
	}

	// Try to read the comment from the .pub file.
	comment := ""
	pubData, err := os.ReadFile(path + ".pub")
	if err == nil {
		_, c, _, _, err := ssh.ParseAuthorizedKey(pubData)
		if err == nil {
			comment = c
		}
	}

	return &sshKeyCandidate{
		PublicKey: signer.PublicKey(),
		Signer:   signer,
		Path:     path,
		Comment:  comment,
	}, nil
}

// RunAuthLogin performs `dbx auth login --server <url>`.
func RunAuthLogin(serverURL string, printer *Printer) error {
	if serverURL == "" {
		return fmt.Errorf("server URL is required (use --server)")
	}

	// Trim trailing slash.
	serverURL = strings.TrimRight(serverURL, "/")

	// Load existing config for TLS CA.
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Discover SSH key.
	candidate, err := discoverSSHKey(cfg.SSHKey)
	if err != nil {
		return fmt.Errorf("no SSH key found: %w", err)
	}
	if candidate.agentConn != nil {
		defer candidate.agentConn.Close()
	}

	fingerprint := ssh.FingerprintSHA256(candidate.PublicKey)
	pubKeyStr := string(ssh.MarshalAuthorizedKey(candidate.PublicKey))
	pubKeyStr = strings.TrimSpace(pubKeyStr)

	if candidate.FromAgent {
		printer.Info("Using SSH key from ssh-agent (%s)", fingerprint)
	} else {
		printer.Info("Using SSH key %s (%s)", candidate.Path, fingerprint)
	}

	// Create an unauthenticated client.
	client, err := NewClient(serverURL, "", cfg.TLSCA)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Try challenge+verify first (existing user).
	challengeResp, err := client.Challenge()
	if err != nil {
		return fmt.Errorf("requesting challenge from server: %w", err)
	}

	nonceBytes, err := hex.DecodeString(challengeResp.Nonce)
	if err != nil {
		return fmt.Errorf("decoding nonce: %w", err)
	}

	sig, err := crypto.SignSSH(candidate.Signer, nonceBytes)
	if err != nil {
		return fmt.Errorf("signing challenge: %w", err)
	}

	verifyResp, err := client.Verify(fingerprint, sig, challengeResp.Nonce)
	if err != nil {
		// If verification failed (fingerprint not registered), register.
		apiErr, ok := err.(*APIError)
		if !ok || apiErr.StatusCode != 401 {
			return fmt.Errorf("authentication failed: %w", err)
		}

		printer.Info("Registering new identity...")

		// Determine name from key comment or hostname.
		name := candidate.Comment
		if name == "" || name == "ssh-agent key" {
			hostname, hErr := os.Hostname()
			if hErr != nil {
				hostname = "unknown"
			}
			name = hostname
		}

		regResp, rErr := client.Register(name, pubKeyStr, fingerprint)
		if rErr != nil {
			return fmt.Errorf("registration failed: %w", rErr)
		}

		printer.Info("Registered as %q (user ID: %s)", regResp.Name, regResp.UserID)

		// Now authenticate with the newly registered key.
		challengeResp2, cErr := client.Challenge()
		if cErr != nil {
			return fmt.Errorf("requesting challenge after registration: %w", cErr)
		}

		nonceBytes2, dErr := hex.DecodeString(challengeResp2.Nonce)
		if dErr != nil {
			return fmt.Errorf("decoding nonce: %w", dErr)
		}

		sig2, sErr := crypto.SignSSH(candidate.Signer, nonceBytes2)
		if sErr != nil {
			return fmt.Errorf("signing challenge: %w", sErr)
		}

		verifyResp, err = client.Verify(fingerprint, sig2, challengeResp2.Nonce)
		if err != nil {
			return fmt.Errorf("verification after registration failed: %w", err)
		}
	}

	// Save config.
	cfg.Server = serverURL
	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	printer.Success("Logged in to %s (session expires in %ds)", serverURL, verifyResp.ExpiresIn)
	return nil
}

// RunAuthStatus performs `dbx auth status`.
func RunAuthStatus(printer *Printer) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	type statusResult struct {
		Server     string `json:"server"`
		AuthMethod string `json:"auth_method"`
		Detail     string `json:"detail"`
	}

	result := statusResult{
		Server: cfg.Server,
	}

	if cfg.Server == "" {
		result.Server = "(not configured)"
	}

	// Determine auth method.
	if token := os.Getenv("DEVBOX_TOKEN"); token != "" {
		result.AuthMethod = "token"
		// Mask the token: show prefix + first 8 chars.
		masked := token
		if len(token) > 16 {
			masked = token[:16] + "..."
		}
		result.Detail = masked
	} else {
		candidate, keyErr := discoverSSHKey(cfg.SSHKey)
		if keyErr != nil {
			result.AuthMethod = "none"
			result.Detail = "no SSH key found"
		} else {
			if candidate.agentConn != nil {
				defer candidate.agentConn.Close()
			}
			if candidate.FromAgent {
				result.AuthMethod = "ssh-agent"
				result.Detail = ssh.FingerprintSHA256(candidate.PublicKey)
			} else {
				result.AuthMethod = "ssh-key"
				result.Detail = candidate.Path + " (" + ssh.FingerprintSHA256(candidate.PublicKey) + ")"
			}
		}
	}

	if printer.JSON {
		return printer.Data(result)
	}

	printer.Info("Server:      %s", result.Server)
	printer.Info("Auth method: %s", result.AuthMethod)
	printer.Info("Detail:      %s", result.Detail)

	return nil
}

// RunAuthLogout performs `dbx auth logout`.
func RunAuthLogout(printer *Printer) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Server == "" {
		printer.Info("Not logged in")
		return nil
	}

	oldServer := cfg.Server
	cfg.Server = ""
	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	printer.Success("Logged out from %s", oldServer)
	return nil
}
