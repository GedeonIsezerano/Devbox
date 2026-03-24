package crypto

import (
	"testing"
)

func TestDeriveKey(t *testing.T) {
	masterKey := []byte("test-master-key-that-is-32-bytes!")

	// Same master + project always produces the same derived key (deterministic).
	key1, err := DeriveKey(masterKey, "project-abc")
	if err != nil {
		t.Fatalf("DeriveKey returned error: %v", err)
	}
	key2, err := DeriveKey(masterKey, "project-abc")
	if err != nil {
		t.Fatalf("DeriveKey returned error: %v", err)
	}

	if len(key1) != len(key2) {
		t.Fatalf("derived keys have different lengths: %d vs %d", len(key1), len(key2))
	}
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatalf("derived keys differ at index %d", i)
		}
	}
}

func TestDeriveKeyDifferentProjects(t *testing.T) {
	masterKey := []byte("test-master-key-that-is-32-bytes!")

	key1, err := DeriveKey(masterKey, "project-abc")
	if err != nil {
		t.Fatalf("DeriveKey returned error: %v", err)
	}
	key2, err := DeriveKey(masterKey, "project-xyz")
	if err != nil {
		t.Fatalf("DeriveKey returned error: %v", err)
	}

	if len(key1) != len(key2) {
		t.Fatalf("derived keys have different lengths: %d vs %d", len(key1), len(key2))
	}

	same := true
	for i := range key1 {
		if key1[i] != key2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("derived keys for different projects should differ")
	}
}

func TestDeriveKeyLength(t *testing.T) {
	masterKey := []byte("test-master-key-that-is-32-bytes!")

	key, err := DeriveKey(masterKey, "project-abc")
	if err != nil {
		t.Fatalf("DeriveKey returned error: %v", err)
	}

	if len(key) != 32 {
		t.Fatalf("expected derived key length 32, got %d", len(key))
	}
}
