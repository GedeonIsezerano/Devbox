package database

import (
	"testing"
)

func TestLogEvent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	entry := AuditEntry{
		UserID:      user.ID,
		ProjectID:   "proj_abc123",
		Environment: "production",
		Action:      "env.push",
		IPAddress:   "10.0.0.1",
		UserAgent:   "devbox-cli/1.0",
	}

	err = LogEvent(db, entry)
	if err != nil {
		t.Fatalf("LogEvent failed: %v", err)
	}

	// Verify the entry was inserted.
	var action, userID, projectID, environment, ipAddress, userAgent string
	err = db.QueryRow(
		"SELECT action, user_id, project_id, environment, ip_address, user_agent FROM audit_log WHERE action = 'env.push'",
	).Scan(&action, &userID, &projectID, &environment, &ipAddress, &userAgent)
	if err != nil {
		t.Fatalf("failed to read back audit entry: %v", err)
	}

	if action != "env.push" {
		t.Fatalf("expected action env.push, got %s", action)
	}
	if userID != user.ID {
		t.Fatalf("expected userID %s, got %s", user.ID, userID)
	}
	if projectID != "proj_abc123" {
		t.Fatalf("expected projectID proj_abc123, got %s", projectID)
	}
	if environment != "production" {
		t.Fatalf("expected environment production, got %s", environment)
	}
	if ipAddress != "10.0.0.1" {
		t.Fatalf("expected ipAddress 10.0.0.1, got %s", ipAddress)
	}
	if userAgent != "devbox-cli/1.0" {
		t.Fatalf("expected userAgent devbox-cli/1.0, got %s", userAgent)
	}
}

func TestLogEventMetadata(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	entry := AuditEntry{
		UserID:   user.ID,
		Action:   "auth.success",
		Metadata: `{"method":"ssh","fingerprint":"SHA256:abc"}`,
	}

	err = LogEvent(db, entry)
	if err != nil {
		t.Fatalf("LogEvent failed: %v", err)
	}

	var metadata string
	err = db.QueryRow(
		"SELECT metadata FROM audit_log WHERE action = 'auth.success'",
	).Scan(&metadata)
	if err != nil {
		t.Fatalf("failed to read back metadata: %v", err)
	}

	if metadata != `{"method":"ssh","fingerprint":"SHA256:abc"}` {
		t.Fatalf("unexpected metadata: %s", metadata)
	}
}

func TestLogEventWithoutOptionalFields(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := AuditEntry{
		Action:    "auth.failure",
		IPAddress: "192.168.1.1",
		UserAgent: "curl/7.0",
	}

	err = LogEvent(db, entry)
	if err != nil {
		t.Fatalf("LogEvent failed: %v", err)
	}

	// Verify that empty-string optional fields were stored as NULL.
	var action string
	var userID, projectID, environment, metadata *string
	err = db.QueryRow(
		"SELECT action, user_id, project_id, environment, metadata FROM audit_log WHERE action = 'auth.failure'",
	).Scan(&action, &userID, &projectID, &environment, &metadata)
	if err != nil {
		t.Fatalf("failed to read back audit entry: %v", err)
	}

	if action != "auth.failure" {
		t.Fatalf("expected action auth.failure, got %s", action)
	}
	if userID != nil {
		t.Fatalf("expected user_id to be NULL, got %v", *userID)
	}
	if projectID != nil {
		t.Fatalf("expected project_id to be NULL, got %v", *projectID)
	}
	if environment != nil {
		t.Fatalf("expected environment to be NULL, got %v", *environment)
	}
	if metadata != nil {
		t.Fatalf("expected metadata to be NULL, got %v", *metadata)
	}
}
