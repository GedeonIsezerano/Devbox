package database

import (
	"database/sql"
	"fmt"
)

// Session represents a row in the sessions table.
type Session struct {
	ID        string
	UserID    string
	ExpiresAt string
	IPAddress string
	UserAgent string
	CreatedAt string
}

// CreateSession inserts a new session with a `ses_` prefix ID and an expiry
// 15 minutes from now.
func CreateSession(db *sql.DB, userID, tokenHash, ipAddress, userAgent string) (Session, error) {
	id := newID("ses_")

	_, err := db.Exec(
		`INSERT INTO sessions (id, user_id, token_hash, expires_at, ip_address, user_agent)
		 VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+15 minutes'), ?, ?)`,
		id, userID, tokenHash, ipAddress, userAgent,
	)
	if err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}

	var sess Session
	var nullIP sql.NullString
	var nullUA sql.NullString
	err = db.QueryRow(
		"SELECT id, user_id, expires_at, ip_address, user_agent, created_at FROM sessions WHERE id = ?", id,
	).Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt, &nullIP, &nullUA, &sess.CreatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("read back session: %w", err)
	}
	if nullIP.Valid {
		sess.IPAddress = nullIP.String
	}
	if nullUA.Valid {
		sess.UserAgent = nullUA.String
	}

	return sess, nil
}

// FindSession looks up a session by its token hash. Only returns sessions
// that have not yet expired. Returns ErrNotFound if no valid session exists.
func FindSession(db *sql.DB, tokenHash string) (Session, error) {
	var sess Session
	var nullIP sql.NullString
	var nullUA sql.NullString
	err := db.QueryRow(
		`SELECT id, user_id, expires_at, ip_address, user_agent, created_at
		 FROM sessions
		 WHERE token_hash = ?
		   AND expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		tokenHash,
	).Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt, &nullIP, &nullUA, &sess.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("find session: %w", err)
	}
	if nullIP.Valid {
		sess.IPAddress = nullIP.String
	}
	if nullUA.Valid {
		sess.UserAgent = nullUA.String
	}
	return sess, nil
}

// DeleteSession removes a session by ID.
func DeleteSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// CleanupExpiredSessions removes all expired sessions and returns the count
// of sessions deleted.
func CleanupExpiredSessions(db *sql.DB) (int64, error) {
	result, err := db.Exec(
		"DELETE FROM sessions WHERE expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now')",
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired sessions: %w", err)
	}
	return result.RowsAffected()
}

// DeleteAllSessions removes all sessions (for emergency-revoke-all) and
// returns the count of sessions deleted.
func DeleteAllSessions(db *sql.DB) (int64, error) {
	result, err := db.Exec("DELETE FROM sessions")
	if err != nil {
		return 0, fmt.Errorf("delete all sessions: %w", err)
	}
	return result.RowsAffected()
}
