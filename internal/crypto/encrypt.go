package crypto

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// Encryptor is the interface for encrypting/decrypting env var blobs.
// Implementations can be swapped (server-side age now, client-side later).
type Encryptor interface {
	Encrypt(plaintext []byte, projectID string) ([]byte, error)
	Decrypt(ciphertext []byte, projectID string) ([]byte, error)
}

// AgeEncryptor implements Encryptor using age encryption with HKDF-derived
// per-project keys. The master key is used with HKDF to derive a unique
// passphrase for each project, which is then used with age's scrypt-based
// passphrase encryption.
type AgeEncryptor struct {
	masterKey []byte
	identity  *age.X25519Identity
}

// NewAgeEncryptor creates a new AgeEncryptor from raw master key bytes.
// The master key is used with HKDF to derive per-project encryption keys.
// An X25519 identity is also generated for key export/import purposes.
func NewAgeEncryptor(masterKey []byte) (*AgeEncryptor, error) {
	if len(masterKey) == 0 {
		return nil, fmt.Errorf("master key must not be empty")
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generate age identity: %w", err)
	}

	return &AgeEncryptor{
		masterKey: append([]byte{}, masterKey...),
		identity:  identity,
	}, nil
}

// NewAgeEncryptorFromFile reads an age identity file and creates an AgeEncryptor.
// The identity's string representation is used as the master key for HKDF derivation.
func NewAgeEncryptorFromFile(path string) (*AgeEncryptor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity file: %w", err)
	}

	return parseIdentity(string(data))
}

// NewAgeEncryptorFromEnv reads the DBX_AGE_KEY environment variable and creates
// an AgeEncryptor. The env var should contain an age identity string.
func NewAgeEncryptorFromEnv() (*AgeEncryptor, error) {
	key := os.Getenv("DBX_AGE_KEY")
	if key == "" {
		return nil, fmt.Errorf("DBX_AGE_KEY environment variable is not set or empty")
	}

	return parseIdentity(key)
}

// GenerateAgeIdentity generates a new age X25519 identity and saves it to the
// given file path with 0600 permissions.
func GenerateAgeIdentity(path string) error {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate age identity: %w", err)
	}

	content := fmt.Sprintf("# created by devbox\n# public key: %s\n%s\n",
		identity.Recipient().String(),
		identity.String(),
	)

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write identity file: %w", err)
	}

	return nil
}

// Identity returns the X25519 identity associated with this encryptor.
func (e *AgeEncryptor) Identity() *age.X25519Identity {
	return e.identity
}

// Encrypt encrypts plaintext using an age scrypt passphrase derived from the
// master key and project ID via HKDF.
func (e *AgeEncryptor) Encrypt(plaintext []byte, projectID string) ([]byte, error) {
	passphrase, err := e.derivePassphrase(projectID)
	if err != nil {
		return nil, fmt.Errorf("derive passphrase: %w", err)
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("create scrypt recipient: %w", err)
	}
	// Use a minimal work factor since the passphrase is already a
	// high-entropy HKDF-derived key (256 bits). Scrypt's computational
	// hardening is unnecessary here.
	recipient.SetWorkFactor(1)

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("age encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("write plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close age writer: %w", err)
	}

	return buf.Bytes(), nil
}

// Decrypt decrypts ciphertext using an age scrypt passphrase derived from the
// master key and project ID via HKDF.
func (e *AgeEncryptor) Decrypt(ciphertext []byte, projectID string) ([]byte, error) {
	passphrase, err := e.derivePassphrase(projectID)
	if err != nil {
		return nil, fmt.Errorf("derive passphrase: %w", err)
	}

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("create scrypt identity: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read decrypted data: %w", err)
	}

	return plaintext, nil
}

// derivePassphrase derives a hex-encoded passphrase from the master key and
// project ID using HKDF.
func (e *AgeEncryptor) derivePassphrase(projectID string) (string, error) {
	derived, err := DeriveKey(e.masterKey, projectID)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(derived), nil
}

// parseIdentity parses an age identity string (possibly with comments) and
// creates an AgeEncryptor using the identity's string representation as the
// master key for HKDF derivation.
func parseIdentity(s string) (*AgeEncryptor, error) {
	// Parse identities, filtering out comments and empty lines.
	identities, err := age.ParseIdentities(strings.NewReader(s))
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}

	if len(identities) == 0 {
		return nil, fmt.Errorf("no age identity found")
	}

	x25519Identity, ok := identities[0].(*age.X25519Identity)
	if !ok {
		return nil, fmt.Errorf("expected X25519 identity, got %T", identities[0])
	}

	// Use the identity's string representation as the master key for HKDF.
	masterKey := []byte(x25519Identity.String())

	return &AgeEncryptor{
		masterKey: masterKey,
		identity:  x25519Identity,
	}, nil
}
