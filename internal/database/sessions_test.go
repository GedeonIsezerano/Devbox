package database

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateSession(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	sess, err := CreateSession(db, user.ID, "sha256sessionhash", "127.0.0.1", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if !strings.HasPrefix(sess.ID, "ses_") {
		t.Fatalf("expected ID to start with ses_, got %s", sess.ID)
	}
	if sess.UserID != user.ID {
		t.Fatalf("expected UserID %s, got %s", user.ID, sess.UserID)
	}
	if sess.IPAddress != "127.0.0.1" {
		t.Fatalf("expected IPAddress 127.0.0.1, got %s", sess.IPAddress)
	}
	if sess.UserAgent != "Mozilla/5.0" {
		t.Fatalf("expected UserAgent Mozilla/5.0, got %s", sess.UserAgent)
	}
	if sess.CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}

	// Verify expiry is approximately 15 minutes from now.
	expiresAt, err := time.Parse(time.RFC3339Nano, sess.ExpiresAt)
	if err != nil {
		t.Fatalf("failed to parse expires_at: %v", err)
	}
	expected := time.Now().UTC().Add(15 * time.Minute)
	diff := expected.Sub(expiresAt)
	if diff < -30*time.Second || diff > 30*time.Second {
		t.Fatalf("expected expires_at ~15 min from now, got %s (diff %v)", sess.ExpiresAt, diff)
	}
}

func TestFindSession(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	tokenHash := "sha256findsession"
	created, err := CreateSession(db, user.ID, tokenHash, "10.0.0.1", "curl/7.0")
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindSession(db, tokenHash)
	if err != nil {
		t.Fatalf("FindSession failed: %v", err)
	}

	if found.ID != created.ID {
		t.Fatalf("expected ID %s, got %s", created.ID, found.ID)
	}
	if found.UserID != user.ID {
		t.Fatalf("expected UserID %s, got %s", user.ID, found.UserID)
	}
	if found.IPAddress != "10.0.0.1" {
		t.Fatalf("expected IPAddress 10.0.0.1, got %s", found.IPAddress)
	}
	if found.UserAgent != "curl/7.0" {
		t.Fatalf("expected UserAgent curl/7.0, got %s", found.UserAgent)
	}
}

func TestFindExpiredSession(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Insert a session with a past expiry directly via SQL.
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	_, err = db.Exec(
		"INSERT INTO sessions (id, user_id, token_hash, expires_at, ip_address, user_agent) VALUES (?, ?, ?, ?, ?, ?)",
		"ses_expired", user.ID, "sha256expiredsession", pastTime, "127.0.0.1", "test",
	)
	if err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	_, err = FindSession(db, "sha256expiredsession")
	if err == nil {
		t.Fatal("expected error for expired session, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	tokenHash := "sha256deletesession"
	sess, err := CreateSession(db, user.ID, tokenHash, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	err = DeleteSession(db, sess.ID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Subsequent find should return ErrNotFound.
	_, err = FindSession(db, tokenHash)
	if err == nil {
		t.Fatal("expected error after deletion, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Create one valid session.
	_, err = CreateSession(db, user.ID, "sha256valid", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Insert two expired sessions directly via SQL.
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	_, err = db.Exec(
		"INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)",
		"ses_exp1", user.ID, "sha256exp1", pastTime,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		"INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)",
		"ses_exp2", user.ID, "sha256exp2", pastTime,
	)
	if err != nil {
		t.Fatal(err)
	}

	count, err := CleanupExpiredSessions(db)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions failed: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 expired sessions deleted, got %d", count)
	}

	// The valid session should still exist.
	_, err = FindSession(db, "sha256valid")
	if err != nil {
		t.Fatalf("valid session should still exist: %v", err)
	}
}

func TestDeleteAllSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Create multiple sessions.
	_, err = CreateSession(db, user.ID, "sha256all1", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateSession(db, user.ID, "sha256all2", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateSession(db, user.ID, "sha256all3", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	count, err := DeleteAllSessions(db)
	if err != nil {
		t.Fatalf("DeleteAllSessions failed: %v", err)
	}

	if count != 3 {
		t.Fatalf("expected 3 sessions deleted, got %d", count)
	}

	// All sessions should be gone.
	_, err = FindSession(db, "sha256all1")
	if err == nil {
		t.Fatal("expected error after delete all, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
