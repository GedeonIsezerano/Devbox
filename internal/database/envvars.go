package database

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrVersionConflict is returned when a push fails due to optimistic lock mismatch.
var ErrVersionConflict = errors.New("version conflict")

// ErrNotFound is returned when the requested env vars do not exist.
var ErrNotFound = errors.New("not found")

// EnvData holds the encrypted blob and version for a project's environment.
type EnvData struct {
	Blob    []byte
	Version int
}

// PushEnvVars stores an encrypted env var blob with optimistic locking.
//
// If expectedVersion is 0, this is treated as the first push and a new row is
// inserted with version=1. Otherwise the existing row is updated only if the
// current version matches expectedVersion; a mismatch returns ErrVersionConflict.
//
// Each push also inserts a history row and prunes history to the latest 10 versions.
func PushEnvVars(db *sql.DB, projectID, environment string, blob []byte, expectedVersion int, userID string) (newVersion int, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if expectedVersion == 0 {
		// First push — insert with version 1.
		id := newID("env_")
		_, err = tx.Exec(
			"INSERT INTO env_vars (id, project_id, environment, blob, version, updated_by) VALUES (?, ?, ?, ?, 1, ?)",
			id, projectID, environment, blob, userID,
		)
		if err != nil {
			return 0, fmt.Errorf("insert env_vars: %w", err)
		}
		newVersion = 1
	} else {
		// Subsequent push — optimistic lock on version.
		result, err := tx.Exec(
			"UPDATE env_vars SET blob = ?, version = version + 1, updated_by = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE project_id = ? AND environment = ? AND version = ?",
			blob, userID, projectID, environment, expectedVersion,
		)
		if err != nil {
			return 0, fmt.Errorf("update env_vars: %w", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("rows affected: %w", err)
		}
		if rowsAffected == 0 {
			return 0, ErrVersionConflict
		}
		newVersion = expectedVersion + 1
	}

	// Insert history row.
	histID := newID("evh_")
	_, err = tx.Exec(
		"INSERT INTO env_var_history (id, project_id, environment, blob, version, created_by) VALUES (?, ?, ?, ?, ?, ?)",
		histID, projectID, environment, blob, newVersion, userID,
	)
	if err != nil {
		return 0, fmt.Errorf("insert env_var_history: %w", err)
	}

	// Prune history to keep only the last 10 versions.
	_, err = tx.Exec(
		`DELETE FROM env_var_history
		 WHERE project_id = ? AND environment = ?
		   AND id NOT IN (
		     SELECT id FROM env_var_history
		     WHERE project_id = ? AND environment = ?
		     ORDER BY version DESC
		     LIMIT 10
		   )`,
		projectID, environment, projectID, environment,
	)
	if err != nil {
		return 0, fmt.Errorf("prune env_var_history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}

	return newVersion, nil
}

// PullEnvVars retrieves the current encrypted blob and version for a project's
// environment. Returns ErrNotFound if no env vars have been pushed yet.
func PullEnvVars(db *sql.DB, projectID, environment string) (EnvData, error) {
	var data EnvData
	err := db.QueryRow(
		"SELECT blob, version FROM env_vars WHERE project_id = ? AND environment = ?",
		projectID, environment,
	).Scan(&data.Blob, &data.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EnvData{}, ErrNotFound
		}
		return EnvData{}, fmt.Errorf("pull env_vars: %w", err)
	}
	return data, nil
}

// GetEnvVersion returns just the current version number for a project's
// environment. Returns ErrNotFound if no env vars have been pushed yet.
func GetEnvVersion(db *sql.DB, projectID, environment string) (int, error) {
	var version int
	err := db.QueryRow(
		"SELECT version FROM env_vars WHERE project_id = ? AND environment = ?",
		projectID, environment,
	).Scan(&version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("get env version: %w", err)
	}
	return version, nil
}
