package database

import (
	"database/sql"
	"fmt"
)

// Token represents a row in the tokens table.
type Token struct {
	ID        string
	UserID    string
	Name      string
	Type      string // "pat" or "provision"
	Scope     string // JSON string: {"permissions":["project:read"],"project_id":"proj_..."}
	ExpiresAt string // may be empty
	SingleUse bool
	LastUsed  string // may be empty
	CreatedAt string
}

// CreateToken inserts a new token with the given SHA-256 hash.
// Returns the created token with a `tok_` prefixed ID.
func CreateToken(db *sql.DB, userID, name, hash, tokenType, scope, expiresAt string, singleUse bool) (Token, error) {
	id := newID("tok_")

	singleUseInt := 0
	if singleUse {
		singleUseInt = 1
	}

	// Use NULL for empty expires_at so the column stays NULL rather than
	// storing an empty string.
	var expiresAtVal any
	if expiresAt != "" {
		expiresAtVal = expiresAt
	}

	_, err := db.Exec(
		"INSERT INTO tokens (id, user_id, name, hash, type, scope, expires_at, single_use) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, userID, name, hash, tokenType, scope, expiresAtVal, singleUseInt,
	)
	if err != nil {
		return Token{}, fmt.Errorf("insert token: %w", err)
	}

	var tok Token
	var nullName sql.NullString
	var nullExpiresAt sql.NullString
	var nullLastUsed sql.NullString
	err = db.QueryRow(
		"SELECT id, user_id, name, type, scope, expires_at, single_use, last_used, created_at FROM tokens WHERE id = ?", id,
	).Scan(&tok.ID, &tok.UserID, &nullName, &tok.Type, &tok.Scope, &nullExpiresAt, &tok.SingleUse, &nullLastUsed, &tok.CreatedAt)
	if err != nil {
		return Token{}, fmt.Errorf("read back token: %w", err)
	}
	if nullName.Valid {
		tok.Name = nullName.String
	}
	if nullExpiresAt.Valid {
		tok.ExpiresAt = nullExpiresAt.String
	}
	if nullLastUsed.Valid {
		tok.LastUsed = nullLastUsed.String
	}

	return tok, nil
}

// FindTokenByHash looks up a token by its SHA-256 hash. Returns ErrNotFound
// if the token does not exist or has expired.
func FindTokenByHash(db *sql.DB, hash string) (Token, error) {
	var tok Token
	var nullName sql.NullString
	var nullExpiresAt sql.NullString
	var nullLastUsed sql.NullString
	err := db.QueryRow(
		`SELECT id, user_id, name, type, scope, expires_at, single_use, last_used, created_at
		 FROM tokens
		 WHERE hash = ?
		   AND (expires_at IS NULL OR expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
		hash,
	).Scan(&tok.ID, &tok.UserID, &nullName, &tok.Type, &tok.Scope, &nullExpiresAt, &tok.SingleUse, &nullLastUsed, &tok.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Token{}, ErrNotFound
		}
		return Token{}, fmt.Errorf("find token by hash: %w", err)
	}
	if nullName.Valid {
		tok.Name = nullName.String
	}
	if nullExpiresAt.Valid {
		tok.ExpiresAt = nullExpiresAt.String
	}
	if nullLastUsed.Valid {
		tok.LastUsed = nullLastUsed.String
	}
	return tok, nil
}

// ConsumeProvisionToken atomically deletes a single-use token by hash and
// returns it. This ensures the token can only be used once. Returns
// ErrNotFound if the token does not exist, has expired, or is not single-use.
func ConsumeProvisionToken(db *sql.DB, hash string) (Token, error) {
	var tok Token
	var nullName sql.NullString
	var nullExpiresAt sql.NullString
	var nullLastUsed sql.NullString
	err := db.QueryRow(
		`DELETE FROM tokens
		 WHERE hash = ?
		   AND single_use = 1
		   AND (expires_at IS NULL OR expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		 RETURNING id, user_id, name, type, scope, expires_at, single_use, last_used, created_at`,
		hash,
	).Scan(&tok.ID, &tok.UserID, &nullName, &tok.Type, &tok.Scope, &nullExpiresAt, &tok.SingleUse, &nullLastUsed, &tok.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Token{}, ErrNotFound
		}
		return Token{}, fmt.Errorf("consume provision token: %w", err)
	}
	if nullName.Valid {
		tok.Name = nullName.String
	}
	if nullExpiresAt.Valid {
		tok.ExpiresAt = nullExpiresAt.String
	}
	if nullLastUsed.Valid {
		tok.LastUsed = nullLastUsed.String
	}
	return tok, nil
}

// ListTokensForUser returns all tokens for the given user. The hash column
// is excluded from the results.
func ListTokensForUser(db *sql.DB, userID string) ([]Token, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, type, scope, expires_at, single_use, last_used, created_at
		 FROM tokens
		 WHERE user_id = ?
		 ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tokens for user: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var tok Token
		var nullName sql.NullString
		var nullExpiresAt sql.NullString
		var nullLastUsed sql.NullString
		if err := rows.Scan(&tok.ID, &tok.UserID, &nullName, &tok.Type, &tok.Scope, &nullExpiresAt, &tok.SingleUse, &nullLastUsed, &tok.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		if nullName.Valid {
			tok.Name = nullName.String
		}
		if nullExpiresAt.Valid {
			tok.ExpiresAt = nullExpiresAt.String
		}
		if nullLastUsed.Valid {
			tok.LastUsed = nullLastUsed.String
		}
		tokens = append(tokens, tok)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tokens: %w", err)
	}

	return tokens, nil
}

// RevokeToken deletes a token by ID.
func RevokeToken(db *sql.DB, tokenID string) error {
	_, err := db.Exec("DELETE FROM tokens WHERE id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

// UpdateLastUsed sets the last_used timestamp to the current time.
func UpdateLastUsed(db *sql.DB, tokenID string) error {
	_, err := db.Exec(
		"UPDATE tokens SET last_used = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?",
		tokenID,
	)
	if err != nil {
		return fmt.Errorf("update last_used: %w", err)
	}
	return nil
}
