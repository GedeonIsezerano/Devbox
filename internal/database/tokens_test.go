package database

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateToken(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	tok, err := CreateToken(db, user.ID, "my-token", "sha256hashvalue", "pat", `{"permissions":["project:read"]}`, "", false)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	if !strings.HasPrefix(tok.ID, "tok_") {
		t.Fatalf("expected ID to start with tok_, got %s", tok.ID)
	}
	if tok.UserID != user.ID {
		t.Fatalf("expected UserID %s, got %s", user.ID, tok.UserID)
	}
	if tok.Name != "my-token" {
		t.Fatalf("expected name my-token, got %s", tok.Name)
	}
	if tok.Type != "pat" {
		t.Fatalf("expected type pat, got %s", tok.Type)
	}
	if tok.Scope != `{"permissions":["project:read"]}` {
		t.Fatalf("unexpected scope: %s", tok.Scope)
	}
	if tok.ExpiresAt != "" {
		t.Fatalf("expected empty expires_at, got %s", tok.ExpiresAt)
	}
	if tok.SingleUse {
		t.Fatal("expected single_use to be false")
	}
	if tok.CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}
}

func TestFindTokenByHash(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	hash := "sha256findhash"
	created, err := CreateToken(db, user.ID, "find-me", hash, "pat", `{"permissions":["project:read"]}`, "", false)
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindTokenByHash(db, hash)
	if err != nil {
		t.Fatalf("FindTokenByHash failed: %v", err)
	}

	if found.ID != created.ID {
		t.Fatalf("expected ID %s, got %s", created.ID, found.ID)
	}
	if found.UserID != user.ID {
		t.Fatalf("expected UserID %s, got %s", user.ID, found.UserID)
	}
	if found.Name != "find-me" {
		t.Fatalf("expected name find-me, got %s", found.Name)
	}
	if found.Type != "pat" {
		t.Fatalf("expected type pat, got %s", found.Type)
	}
}

func TestFindTokenByHashNotFound(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = FindTokenByHash(db, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestFindTokenByHashExpired(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Create a token with an expiration time in the past.
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	hash := "sha256expiredhash"
	_, err = CreateToken(db, user.ID, "expired-token", hash, "pat", `{"permissions":["project:read"]}`, pastTime, false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FindTokenByHash(db, hash)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestConsumeProvisionToken(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	hash := "sha256provisionhash"
	created, err := CreateToken(db, user.ID, "provision-token", hash, "provision", `{"permissions":["project:read"]}`, "", true)
	if err != nil {
		t.Fatal(err)
	}

	// First consume should succeed.
	consumed, err := ConsumeProvisionToken(db, hash)
	if err != nil {
		t.Fatalf("first ConsumeProvisionToken failed: %v", err)
	}
	if consumed.ID != created.ID {
		t.Fatalf("expected ID %s, got %s", created.ID, consumed.ID)
	}
	if consumed.UserID != user.ID {
		t.Fatalf("expected UserID %s, got %s", user.ID, consumed.UserID)
	}

	// Second consume should fail (token already consumed/deleted).
	_, err = ConsumeProvisionToken(db, hash)
	if err == nil {
		t.Fatal("expected error on second consume, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestListTokensForUser(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Create two tokens.
	_, err = CreateToken(db, user.ID, "token-one", "hash1", "pat", `{}`, "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateToken(db, user.ID, "token-two", "hash2", "pat", `{}`, "", false)
	if err != nil {
		t.Fatal(err)
	}

	tokens, err := ListTokensForUser(db, user.ID)
	if err != nil {
		t.Fatalf("ListTokensForUser failed: %v", err)
	}

	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}

	// Verify tokens have the correct names and IDs are present.
	names := map[string]bool{}
	for _, tok := range tokens {
		names[tok.Name] = true
		if !strings.HasPrefix(tok.ID, "tok_") {
			t.Fatalf("expected ID with tok_ prefix, got %s", tok.ID)
		}
		if tok.UserID != user.ID {
			t.Fatalf("expected UserID %s, got %s", user.ID, tok.UserID)
		}
	}
	if !names["token-one"] || !names["token-two"] {
		t.Fatalf("expected token-one and token-two, got %v", names)
	}
}

func TestRevokeToken(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	tok, err := CreateToken(db, user.ID, "revoke-me", "hashrevoke", "pat", `{}`, "", false)
	if err != nil {
		t.Fatal(err)
	}

	err = RevokeToken(db, tok.ID)
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Verify the token is gone.
	_, err = FindTokenByHash(db, "hashrevoke")
	if err == nil {
		t.Fatal("expected error after revocation, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestUpdateLastUsed(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	tok, err := CreateToken(db, user.ID, "update-last-used", "hashlastused", "pat", `{}`, "", false)
	if err != nil {
		t.Fatal(err)
	}

	// Initially last_used should be empty.
	if tok.LastUsed != "" {
		t.Fatalf("expected empty last_used initially, got %s", tok.LastUsed)
	}

	err = UpdateLastUsed(db, tok.ID)
	if err != nil {
		t.Fatalf("UpdateLastUsed failed: %v", err)
	}

	// Verify last_used is now set by finding the token.
	found, err := FindTokenByHash(db, "hashlastused")
	if err != nil {
		t.Fatalf("FindTokenByHash after update failed: %v", err)
	}
	if found.LastUsed == "" {
		t.Fatal("expected last_used to be set after UpdateLastUsed")
	}

	// Verify it looks like a valid timestamp.
	_, parseErr := time.Parse(time.RFC3339Nano, found.LastUsed)
	if parseErr != nil {
		t.Fatalf("last_used is not a valid timestamp: %s (err: %v)", found.LastUsed, parseErr)
	}
}
