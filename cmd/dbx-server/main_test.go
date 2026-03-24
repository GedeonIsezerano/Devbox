package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/devbox/internal/database"
)

func TestBackupCreatesValidCopy(t *testing.T) {
	// Create a temp directory for test files.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.db")
	backupPath := filepath.Join(tmpDir, "backup.db")

	// Open source DB and insert some data.
	srcDB, err := database.Open(srcPath)
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}

	user, err := database.CreateUser(srcDB, "testuser")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = database.CreateToken(srcDB, user.ID, "test-token", "abc123hash", "pat", `{"permissions":["project:read"]}`, "", false)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	srcDB.Close()

	// Run the backup.
	if err := runBackup(srcPath, backupPath); err != nil {
		t.Fatalf("runBackup: %v", err)
	}

	// Verify backup file exists.
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file not found: %v", err)
	}

	// Open the backup and verify data is there.
	backupDB, err := database.Open(backupPath)
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer backupDB.Close()

	// Verify user exists.
	var name string
	err = backupDB.QueryRow("SELECT name FROM users WHERE id = ?", user.ID).Scan(&name)
	if err != nil {
		t.Fatalf("user not found in backup: %v", err)
	}
	if name != "testuser" {
		t.Fatalf("expected name 'testuser', got '%s'", name)
	}

	// Verify token exists.
	var tokenCount int
	err = backupDB.QueryRow("SELECT COUNT(*) FROM tokens WHERE user_id = ?", user.ID).Scan(&tokenCount)
	if err != nil {
		t.Fatalf("count tokens in backup: %v", err)
	}
	if tokenCount != 1 {
		t.Fatalf("expected 1 token in backup, got %d", tokenCount)
	}
}

func TestEmergencyRevokeAllRemovesEverything(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "revoke-test.db")

	// Open DB and insert sessions and tokens.
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	user, err := database.CreateUser(db, "testuser")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create some tokens.
	for i := 0; i < 3; i++ {
		_, err := database.CreateToken(db, user.ID, "token", "hash"+string(rune('0'+i)), "pat", "{}", "", false)
		if err != nil {
			t.Fatalf("create token %d: %v", i, err)
		}
	}

	// Create some sessions.
	for i := 0; i < 2; i++ {
		_, err := database.CreateSession(db, user.ID, "session_hash"+string(rune('0'+i)), "127.0.0.1", "test-agent")
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	// Verify data exists.
	var tokenCount, sessionCount int
	db.QueryRow("SELECT COUNT(*) FROM tokens").Scan(&tokenCount)
	db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
	if tokenCount != 3 {
		t.Fatalf("expected 3 tokens before revoke, got %d", tokenCount)
	}
	if sessionCount != 2 {
		t.Fatalf("expected 2 sessions before revoke, got %d", sessionCount)
	}

	db.Close()

	// Run emergency revoke all.
	if err := runEmergencyRevokeAll(dbPath); err != nil {
		t.Fatalf("runEmergencyRevokeAll: %v", err)
	}

	// Reopen and verify everything is gone.
	db, err = database.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db.Close()

	db.QueryRow("SELECT COUNT(*) FROM tokens").Scan(&tokenCount)
	db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
	if tokenCount != 0 {
		t.Fatalf("expected 0 tokens after revoke, got %d", tokenCount)
	}
	if sessionCount != 0 {
		t.Fatalf("expected 0 sessions after revoke, got %d", sessionCount)
	}

	// Verify audit log was written.
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action = 'emergency.revoke_all'").Scan(&auditCount)
	if auditCount != 1 {
		t.Fatalf("expected 1 audit log entry for emergency.revoke_all, got %d", auditCount)
	}
}
