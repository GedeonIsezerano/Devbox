package database

import (
	crypto_rand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// User represents a row in the users table.
type User struct {
	ID        string
	Name      string
	IsAdmin   bool
	CreatedAt string
}

// newID generates a prefixed random ID (e.g. "usr_" + 32 hex chars).
func newID(prefix string) string {
	b := make([]byte, 16)
	crypto_rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// CreateUser inserts a new user. The first user in the database is
// automatically promoted to admin.
func CreateUser(db *sql.DB, name string) (User, error) {
	id := newID("usr_")

	// Check if this will be the first user.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return User{}, fmt.Errorf("count users: %w", err)
	}

	isAdmin := 0
	if count == 0 {
		isAdmin = 1
	}

	_, err := db.Exec(
		"INSERT INTO users (id, name, is_admin) VALUES (?, ?, ?)",
		id, name, isAdmin,
	)
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	var user User
	err = db.QueryRow(
		"SELECT id, name, is_admin, created_at FROM users WHERE id = ?", id,
	).Scan(&user.ID, &user.Name, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("read back user: %w", err)
	}

	return user, nil
}

// AddSSHKey inserts a new SSH key for the given user.
func AddSSHKey(db *sql.DB, userID, fingerprint, publicKey, name string) error {
	id := newID("key_")
	_, err := db.Exec(
		"INSERT INTO ssh_keys (id, user_id, fingerprint, public_key, name) VALUES (?, ?, ?, ?, ?)",
		id, userID, fingerprint, publicKey, name,
	)
	if err != nil {
		return fmt.Errorf("insert ssh key: %w", err)
	}
	return nil
}

// FindUserByFingerprint looks up the user that owns the SSH key with the
// given fingerprint. Returns sql.ErrNoRows if no match is found.
func FindUserByFingerprint(db *sql.DB, fingerprint string) (User, error) {
	var user User
	err := db.QueryRow(
		`SELECT u.id, u.name, u.is_admin, u.created_at
		 FROM users u
		 JOIN ssh_keys k ON k.user_id = u.id
		 WHERE k.fingerprint = ?`, fingerprint,
	).Scan(&user.ID, &user.Name, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("find user by fingerprint: %w", err)
	}
	return user, nil
}
