package database

import (
	"strings"
	"testing"
)

func TestCreateUser(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if !strings.HasPrefix(user.ID, "usr_") {
		t.Fatalf("expected ID to start with usr_, got %s", user.ID)
	}
	if user.Name != "alice" {
		t.Fatalf("expected name alice, got %s", user.Name)
	}
	if user.CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}
}

func TestCreateUserFirstIsAdmin(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "first-user")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if !user.IsAdmin {
		t.Fatal("expected first user to be admin")
	}
}

func TestCreateUserSecondIsNotAdmin(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = CreateUser(db, "first-user")
	if err != nil {
		t.Fatalf("CreateUser (first) failed: %v", err)
	}

	second, err := CreateUser(db, "second-user")
	if err != nil {
		t.Fatalf("CreateUser (second) failed: %v", err)
	}

	if second.IsAdmin {
		t.Fatal("expected second user to NOT be admin")
	}
}

func TestAddSSHKey(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	err = AddSSHKey(db, user.ID, "SHA256:abc123", "ssh-ed25519 AAAA...", "my-laptop")
	if err != nil {
		t.Fatalf("AddSSHKey failed: %v", err)
	}

	// Verify the key was inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM ssh_keys WHERE user_id = ?", user.ID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 SSH key, got %d", count)
	}
}

func TestFindUserByFingerprint(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	fingerprint := "SHA256:findme123"
	err = AddSSHKey(db, user.ID, fingerprint, "ssh-ed25519 AAAA...", "my-laptop")
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindUserByFingerprint(db, fingerprint)
	if err != nil {
		t.Fatalf("FindUserByFingerprint failed: %v", err)
	}

	if found.ID != user.ID {
		t.Fatalf("expected user ID %s, got %s", user.ID, found.ID)
	}
	if found.Name != "alice" {
		t.Fatalf("expected name alice, got %s", found.Name)
	}
	if !found.IsAdmin {
		t.Fatal("expected found user to be admin (first user)")
	}
}

func TestAddDuplicateFingerprint(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	fingerprint := "SHA256:duplicate"
	err = AddSSHKey(db, user.ID, fingerprint, "ssh-ed25519 AAAA...", "key1")
	if err != nil {
		t.Fatal(err)
	}

	err = AddSSHKey(db, user.ID, fingerprint, "ssh-ed25519 BBBB...", "key2")
	if err == nil {
		t.Fatal("expected error on duplicate fingerprint, got nil")
	}
}
