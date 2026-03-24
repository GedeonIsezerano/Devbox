package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateToken generates a cryptographically random token with the given prefix.
// It generates 32 random bytes and hex-encodes them, then prepends the prefix.
// Common prefixes are "dbx_pat_" for personal access tokens and "dbx_prov_" for
// provisioning tokens.
func GenerateToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

// HashToken returns the hex-encoded SHA-256 hash of the given token string.
// This is used for storing token hashes in the database rather than raw tokens.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
