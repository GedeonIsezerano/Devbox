package crypto

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("failed to generate master key: %v", err)
	}

	enc, err := NewAgeEncryptor(masterKey)
	if err != nil {
		t.Fatalf("NewAgeEncryptor returned error: %v", err)
	}

	plaintext := []byte("hello, this is secret data")
	projectID := "project-123"

	ciphertext, err := enc.Encrypt(plaintext, projectID)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	if bytes.Equal(plaintext, ciphertext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	decrypted, err := enc.Decrypt(ciphertext, projectID)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted data does not match original: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	if _, err := rand.Read(key1); err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}

	enc1, err := NewAgeEncryptor(key1)
	if err != nil {
		t.Fatalf("NewAgeEncryptor(key1) returned error: %v", err)
	}
	enc2, err := NewAgeEncryptor(key2)
	if err != nil {
		t.Fatalf("NewAgeEncryptor(key2) returned error: %v", err)
	}

	plaintext := []byte("secret data")
	projectID := "project-123"

	ciphertext, err := enc1.Encrypt(plaintext, projectID)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	_, err = enc2.Decrypt(ciphertext, projectID)
	if err == nil {
		t.Fatal("Decrypt with wrong key should return error")
	}
}

func TestEncryptorInterface(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("failed to generate master key: %v", err)
	}

	// Verify AgeEncryptor satisfies the Encryptor interface.
	var enc Encryptor
	var err error
	enc, err = NewAgeEncryptor(masterKey)
	if err != nil {
		t.Fatalf("NewAgeEncryptor returned error: %v", err)
	}

	plaintext := []byte("interface test data")
	projectID := "project-456"

	ciphertext, err := enc.Encrypt(plaintext, projectID)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext, projectID)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptEmptyData(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("failed to generate master key: %v", err)
	}

	enc, err := NewAgeEncryptor(masterKey)
	if err != nil {
		t.Fatalf("NewAgeEncryptor returned error: %v", err)
	}

	plaintext := []byte{}
	projectID := "project-789"

	ciphertext, err := enc.Encrypt(plaintext, projectID)
	if err != nil {
		t.Fatalf("Encrypt returned error for empty data: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext, projectID)
	if err != nil {
		t.Fatalf("Decrypt returned error for empty data: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("round-trip of empty data failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptLargeData(t *testing.T) {
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("failed to generate master key: %v", err)
	}

	enc, err := NewAgeEncryptor(masterKey)
	if err != nil {
		t.Fatalf("NewAgeEncryptor returned error: %v", err)
	}

	// 64KB payload.
	plaintext := make([]byte, 64*1024)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatalf("failed to generate large payload: %v", err)
	}
	projectID := "project-large"

	ciphertext, err := enc.Encrypt(plaintext, projectID)
	if err != nil {
		t.Fatalf("Encrypt returned error for large data: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext, projectID)
	if err != nil {
		t.Fatalf("Decrypt returned error for large data: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatal("round-trip of large data failed: decrypted data does not match original")
	}
}

func TestNewAgeEncryptorFromEnv(t *testing.T) {
	// Generate an identity to use as the env var value.
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("failed to generate master key: %v", err)
	}

	enc1, err := NewAgeEncryptor(masterKey)
	if err != nil {
		t.Fatalf("NewAgeEncryptor returned error: %v", err)
	}

	// Set the env var to the identity string.
	t.Setenv("DBX_AGE_KEY", enc1.Identity().String())

	enc2, err := NewAgeEncryptorFromEnv()
	if err != nil {
		t.Fatalf("NewAgeEncryptorFromEnv returned error: %v", err)
	}

	// Both encryptors should produce the same recipient (same underlying identity).
	if enc1.Identity().Recipient().String() != enc2.Identity().Recipient().String() {
		t.Fatal("identity from env should match original identity")
	}
}

func TestNewAgeEncryptorFromEnvMissing(t *testing.T) {
	t.Setenv("DBX_AGE_KEY", "")
	_, err := NewAgeEncryptorFromEnv()
	if err == nil {
		t.Fatal("NewAgeEncryptorFromEnv should return error when env var is empty")
	}
}

func TestGenerateAgeIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "age-key.txt")

	err := GenerateAgeIdentity(path)
	if err != nil {
		t.Fatalf("GenerateAgeIdentity returned error: %v", err)
	}

	// File should exist.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("identity file not found: %v", err)
	}

	// File should be owner-only readable/writable (0600).
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("expected file permissions 0600, got %o", perm)
	}

	// Should be able to load from the file.
	enc, err := NewAgeEncryptorFromFile(path)
	if err != nil {
		t.Fatalf("NewAgeEncryptorFromFile returned error: %v", err)
	}

	// Round-trip test with the loaded identity.
	plaintext := []byte("test from generated identity")
	ciphertext, err := enc.Encrypt(plaintext, "test-project")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	decrypted, err := enc.Decrypt(ciphertext, "test-project")
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Fatal("round-trip with generated identity failed")
	}
}
