package crypto

import (
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// DeriveKey derives a 32-byte key from a master key and project ID using
// HKDF-SHA256. The project ID is used as the salt, and a fixed info string
// provides domain separation.
func DeriveKey(masterKey []byte, projectID string) ([]byte, error) {
	r := hkdf.New(sha256.New, masterKey, []byte(projectID), []byte("devbox-project-encryption"))

	derived := make([]byte, 32)
	if _, err := io.ReadFull(r, derived); err != nil {
		return nil, fmt.Errorf("hkdf derive: %w", err)
	}

	return derived, nil
}
