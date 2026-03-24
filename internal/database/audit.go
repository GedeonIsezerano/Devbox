package database

import (
	"database/sql"
	"fmt"
)

// AuditEntry represents a row to insert into the audit_log table.
type AuditEntry struct {
	UserID      string // may be empty
	ProjectID   string // may be empty
	Environment string // may be empty
	Action      string // required: auth.success, auth.failure, env.pull, env.push, etc.
	Metadata    string // JSON string, may be empty
	IPAddress   string
	UserAgent   string
}

// nullableString converts an empty string to a NULL-able value for SQL inserts.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// LogEvent inserts an audit log entry. Empty strings for optional fields
// (UserID, ProjectID, Environment, Metadata) are stored as NULL.
func LogEvent(db *sql.DB, entry AuditEntry) error {
	_, err := db.Exec(
		`INSERT INTO audit_log (user_id, project_id, environment, action, metadata, ip_address, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nullableString(entry.UserID),
		nullableString(entry.ProjectID),
		nullableString(entry.Environment),
		entry.Action,
		nullableString(entry.Metadata),
		entry.IPAddress,
		entry.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("insert audit_log: %w", err)
	}
	return nil
}
