package database

import (
	"database/sql"
	"fmt"
)

// Project represents a row in the projects table.
type Project struct {
	ID        string
	Name      string
	RemoteURL string // may be empty for manually named projects
	EnvFile   string
	OwnerID   string
	CreatedAt string
}

// CreateProject inserts a new project and adds the owner as an admin member.
func CreateProject(db *sql.DB, name, remoteURL, envFile, ownerID string) (Project, error) {
	id := newID("proj_")

	// Use NULL for empty remote_url so the UNIQUE constraint allows multiple
	// projects without a remote URL.
	var remoteURLVal any
	if remoteURL != "" {
		remoteURLVal = remoteURL
	}

	tx, err := db.Begin()
	if err != nil {
		return Project{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT INTO projects (id, name, remote_url, env_file, owner_id) VALUES (?, ?, ?, ?, ?)",
		id, name, remoteURLVal, envFile, ownerID,
	)
	if err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}

	_, err = tx.Exec(
		"INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)",
		id, ownerID, "admin",
	)
	if err != nil {
		return Project{}, fmt.Errorf("insert project member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit tx: %w", err)
	}

	var proj Project
	var rawRemoteURL sql.NullString
	err = db.QueryRow(
		"SELECT id, name, remote_url, env_file, owner_id, created_at FROM projects WHERE id = ?", id,
	).Scan(&proj.ID, &proj.Name, &rawRemoteURL, &proj.EnvFile, &proj.OwnerID, &proj.CreatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("read back project: %w", err)
	}
	if rawRemoteURL.Valid {
		proj.RemoteURL = rawRemoteURL.String
	}

	return proj, nil
}

// FindProjectByID looks up a project by its primary key.
// Returns sql.ErrNoRows (wrapped) if no match is found.
func FindProjectByID(db *sql.DB, id string) (Project, error) {
	var proj Project
	var rawRemoteURL sql.NullString
	err := db.QueryRow(
		"SELECT id, name, remote_url, env_file, owner_id, created_at FROM projects WHERE id = ?",
		id,
	).Scan(&proj.ID, &proj.Name, &rawRemoteURL, &proj.EnvFile, &proj.OwnerID, &proj.CreatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("find project by id: %w", err)
	}
	if rawRemoteURL.Valid {
		proj.RemoteURL = rawRemoteURL.String
	}
	return proj, nil
}

// FindProjectByRemoteURL looks up a project by its remote_url column.
// Returns sql.ErrNoRows (wrapped) if no match is found.
func FindProjectByRemoteURL(db *sql.DB, remoteURL string) (Project, error) {
	var proj Project
	var rawRemoteURL sql.NullString
	err := db.QueryRow(
		"SELECT id, name, remote_url, env_file, owner_id, created_at FROM projects WHERE remote_url = ?",
		remoteURL,
	).Scan(&proj.ID, &proj.Name, &rawRemoteURL, &proj.EnvFile, &proj.OwnerID, &proj.CreatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("find project by remote URL: %w", err)
	}
	if rawRemoteURL.Valid {
		proj.RemoteURL = rawRemoteURL.String
	}
	return proj, nil
}

// FindProjectByName looks up a project by name (exact, case-sensitive match).
// Returns sql.ErrNoRows (wrapped) if no match is found.
func FindProjectByName(db *sql.DB, name string) (Project, error) {
	var proj Project
	var rawRemoteURL sql.NullString
	err := db.QueryRow(
		"SELECT id, name, remote_url, env_file, owner_id, created_at FROM projects WHERE name = ?",
		name,
	).Scan(&proj.ID, &proj.Name, &rawRemoteURL, &proj.EnvFile, &proj.OwnerID, &proj.CreatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("find project by name: %w", err)
	}
	if rawRemoteURL.Valid {
		proj.RemoteURL = rawRemoteURL.String
	}
	return proj, nil
}

// ListProjectsForUser returns all projects the user is a member of.
func ListProjectsForUser(db *sql.DB, userID string) ([]Project, error) {
	rows, err := db.Query(
		`SELECT p.id, p.name, p.remote_url, p.env_file, p.owner_id, p.created_at
		 FROM projects p
		 JOIN project_members pm ON pm.project_id = p.id
		 WHERE pm.user_id = ?
		 ORDER BY p.created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list projects for user: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var proj Project
		var rawRemoteURL sql.NullString
		if err := rows.Scan(&proj.ID, &proj.Name, &rawRemoteURL, &proj.EnvFile, &proj.OwnerID, &proj.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		if rawRemoteURL.Valid {
			proj.RemoteURL = rawRemoteURL.String
		}
		projects = append(projects, proj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}

	return projects, nil
}

// DeleteProject removes a project and all related rows (project_members,
// env_vars, env_var_history). These are deleted manually because the schema
// does not use ON DELETE CASCADE.
func DeleteProject(db *sql.DB, projectID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete dependent rows first (order matters for foreign keys).
	for _, stmt := range []string{
		"DELETE FROM env_var_history WHERE project_id = ?",
		"DELETE FROM env_vars WHERE project_id = ?",
		"DELETE FROM project_members WHERE project_id = ?",
		"DELETE FROM projects WHERE id = ?",
	} {
		if _, err := tx.Exec(stmt, projectID); err != nil {
			return fmt.Errorf("delete project (%s): %w", stmt, err)
		}
	}

	return tx.Commit()
}

// roleLevel returns a numeric level for the role hierarchy.
// admin(3) > writer(2) > reader(1).
func roleLevel(role string) int {
	switch role {
	case "admin":
		return 3
	case "writer":
		return 2
	case "reader":
		return 1
	default:
		return 0
	}
}

// CheckMembership verifies that the user has at least the required role in
// the project. Role hierarchy: admin > writer > reader.
// Returns nil if the user meets the requirement, or an error otherwise.
func CheckMembership(db *sql.DB, projectID, userID, requiredRole string) error {
	var actualRole string
	err := db.QueryRow(
		"SELECT role FROM project_members WHERE project_id = ? AND user_id = ?",
		projectID, userID,
	).Scan(&actualRole)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}

	if roleLevel(actualRole) < roleLevel(requiredRole) {
		return fmt.Errorf("insufficient role: have %s, need %s", actualRole, requiredRole)
	}

	return nil
}

// UpdateProjectEnvFile updates the env_file field for a project.
// Returns an error if the project does not exist.
func UpdateProjectEnvFile(db *sql.DB, projectID, envFile string) error {
	result, err := db.Exec(
		"UPDATE projects SET env_file = ? WHERE id = ?",
		envFile, projectID,
	)
	if err != nil {
		return fmt.Errorf("update project env_file: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("update project env_file: project %s not found", projectID)
	}

	return nil
}
