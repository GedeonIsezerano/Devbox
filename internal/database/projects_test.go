package database

import (
	"database/sql"
	"strings"
	"testing"
)

func TestCreateProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	proj, err := CreateProject(db, "my-project", "", ".env", user.ID)
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	if !strings.HasPrefix(proj.ID, "proj_") {
		t.Fatalf("expected ID to start with proj_, got %s", proj.ID)
	}
	if proj.Name != "my-project" {
		t.Fatalf("expected name my-project, got %s", proj.Name)
	}
	if proj.OwnerID != user.ID {
		t.Fatalf("expected owner_id %s, got %s", user.ID, proj.OwnerID)
	}
	if proj.CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}
	if proj.EnvFile != ".env" {
		t.Fatalf("expected env_file .env, got %s", proj.EnvFile)
	}

	// Verify creator was auto-added as admin member
	var role string
	err = db.QueryRow(
		"SELECT role FROM project_members WHERE project_id = ? AND user_id = ?",
		proj.ID, user.ID,
	).Scan(&role)
	if err != nil {
		t.Fatalf("expected creator to be in project_members: %v", err)
	}
	if role != "admin" {
		t.Fatalf("expected creator role to be admin, got %s", role)
	}
}

func TestCreateProjectWithRemoteURL(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	remoteURL := "https://github.com/user/repo.git"
	proj, err := CreateProject(db, "my-repo", remoteURL, ".env", user.ID)
	if err != nil {
		t.Fatalf("CreateProject with remote URL failed: %v", err)
	}

	if proj.RemoteURL != remoteURL {
		t.Fatalf("expected remote_url %s, got %s", remoteURL, proj.RemoteURL)
	}
}

func TestFindProjectByRemoteURL(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	remoteURL := "https://github.com/user/repo.git"
	created, err := CreateProject(db, "my-repo", remoteURL, ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectByRemoteURL(db, remoteURL)
	if err != nil {
		t.Fatalf("FindProjectByRemoteURL failed: %v", err)
	}

	if found.ID != created.ID {
		t.Fatalf("expected project ID %s, got %s", created.ID, found.ID)
	}
	if found.Name != "my-repo" {
		t.Fatalf("expected name my-repo, got %s", found.Name)
	}
	if found.RemoteURL != remoteURL {
		t.Fatalf("expected remote_url %s, got %s", remoteURL, found.RemoteURL)
	}

	// Test not found
	_, err = FindProjectByRemoteURL(db, "https://github.com/nonexistent/repo.git")
	if err == nil {
		t.Fatal("expected error for nonexistent remote URL, got nil")
	}
}

func TestFindProjectByName(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	created, err := CreateProject(db, "my-project", "", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectByName(db, "my-project")
	if err != nil {
		t.Fatalf("FindProjectByName failed: %v", err)
	}

	if found.ID != created.ID {
		t.Fatalf("expected project ID %s, got %s", created.ID, found.ID)
	}

	// Case-sensitive: should not find with different case
	_, err = FindProjectByName(db, "My-Project")
	if err == nil {
		t.Fatal("expected error for case-mismatched name, got nil")
	}

	// Not found
	_, err = FindProjectByName(db, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent name, got nil")
	}
}

func TestListProjectsForUser(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	alice, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	bob, err := CreateUser(db, "bob")
	if err != nil {
		t.Fatal(err)
	}

	// Alice creates two projects
	_, err = CreateProject(db, "alice-proj-1", "", ".env", alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateProject(db, "alice-proj-2", "", ".env", alice.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Bob creates one project
	_, err = CreateProject(db, "bob-proj-1", "", ".env", bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Alice should see only her 2 projects
	aliceProjects, err := ListProjectsForUser(db, alice.ID)
	if err != nil {
		t.Fatalf("ListProjectsForUser failed: %v", err)
	}
	if len(aliceProjects) != 2 {
		t.Fatalf("expected 2 projects for alice, got %d", len(aliceProjects))
	}

	// Bob should see only his 1 project
	bobProjects, err := ListProjectsForUser(db, bob.ID)
	if err != nil {
		t.Fatalf("ListProjectsForUser failed: %v", err)
	}
	if len(bobProjects) != 1 {
		t.Fatalf("expected 1 project for bob, got %d", len(bobProjects))
	}

	// A non-member user should see 0 projects
	charlie, err := CreateUser(db, "charlie")
	if err != nil {
		t.Fatal(err)
	}
	charlieProjects, err := ListProjectsForUser(db, charlie.ID)
	if err != nil {
		t.Fatalf("ListProjectsForUser failed: %v", err)
	}
	if len(charlieProjects) != 0 {
		t.Fatalf("expected 0 projects for charlie, got %d", len(charlieProjects))
	}
}

func TestDeleteProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	proj, err := CreateProject(db, "doomed-project", "", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Insert related env_vars and env_var_history rows to verify cascade cleanup
	_, err = db.Exec(
		"INSERT INTO env_vars (id, project_id, environment, blob, version, updated_by) VALUES (?, ?, ?, ?, ?, ?)",
		newID("env_"), proj.ID, "default", []byte("data"), 1, user.ID,
	)
	if err != nil {
		t.Fatalf("insert env_var: %v", err)
	}
	_, err = db.Exec(
		"INSERT INTO env_var_history (id, project_id, environment, blob, version, created_by) VALUES (?, ?, ?, ?, ?, ?)",
		newID("evh_"), proj.ID, "default", []byte("data"), 1, user.ID,
	)
	if err != nil {
		t.Fatalf("insert env_var_history: %v", err)
	}

	err = DeleteProject(db, proj.ID)
	if err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}

	// Verify project is gone
	_, err = FindProjectByName(db, "doomed-project")
	if err == nil {
		t.Fatal("expected error finding deleted project, got nil")
	}

	// Verify project_members are gone
	var memberCount int
	err = db.QueryRow("SELECT COUNT(*) FROM project_members WHERE project_id = ?", proj.ID).Scan(&memberCount)
	if err != nil {
		t.Fatal(err)
	}
	if memberCount != 0 {
		t.Fatalf("expected 0 project_members after delete, got %d", memberCount)
	}

	// Verify env_vars are gone
	var envCount int
	err = db.QueryRow("SELECT COUNT(*) FROM env_vars WHERE project_id = ?", proj.ID).Scan(&envCount)
	if err != nil {
		t.Fatal(err)
	}
	if envCount != 0 {
		t.Fatalf("expected 0 env_vars after delete, got %d", envCount)
	}

	// Verify env_var_history is gone
	var histCount int
	err = db.QueryRow("SELECT COUNT(*) FROM env_var_history WHERE project_id = ?", proj.ID).Scan(&histCount)
	if err != nil {
		t.Fatal(err)
	}
	if histCount != 0 {
		t.Fatalf("expected 0 env_var_history after delete, got %d", histCount)
	}
}

func TestCheckMembership(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	alice, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	bob, err := CreateUser(db, "bob")
	if err != nil {
		t.Fatal(err)
	}

	charlie, err := CreateUser(db, "charlie")
	if err != nil {
		t.Fatal(err)
	}

	outsider, err := CreateUser(db, "outsider")
	if err != nil {
		t.Fatal(err)
	}

	proj, err := CreateProject(db, "team-project", "", ".env", alice.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Add bob as writer
	_, err = db.Exec(
		"INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)",
		proj.ID, bob.ID, "writer",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Add charlie as reader
	_, err = db.Exec(
		"INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)",
		proj.ID, charlie.ID, "reader",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Admin (alice) passes all checks
	for _, role := range []string{"reader", "writer", "admin"} {
		if err := CheckMembership(db, proj.ID, alice.ID, role); err != nil {
			t.Fatalf("admin should pass %s check: %v", role, err)
		}
	}

	// Writer (bob) passes writer and reader, fails admin
	if err := CheckMembership(db, proj.ID, bob.ID, "reader"); err != nil {
		t.Fatalf("writer should pass reader check: %v", err)
	}
	if err := CheckMembership(db, proj.ID, bob.ID, "writer"); err != nil {
		t.Fatalf("writer should pass writer check: %v", err)
	}
	if err := CheckMembership(db, proj.ID, bob.ID, "admin"); err == nil {
		t.Fatal("writer should fail admin check")
	}

	// Reader (charlie) passes reader, fails writer and admin
	if err := CheckMembership(db, proj.ID, charlie.ID, "reader"); err != nil {
		t.Fatalf("reader should pass reader check: %v", err)
	}
	if err := CheckMembership(db, proj.ID, charlie.ID, "writer"); err == nil {
		t.Fatal("reader should fail writer check")
	}
	if err := CheckMembership(db, proj.ID, charlie.ID, "admin"); err == nil {
		t.Fatal("reader should fail admin check")
	}

	// Non-member (outsider) fails all checks
	if err := CheckMembership(db, proj.ID, outsider.ID, "reader"); err == nil {
		t.Fatal("non-member should fail reader check")
	}
}

func TestUpdateProjectEnvFile(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := CreateUser(db, "alice")
	if err != nil {
		t.Fatal(err)
	}

	proj, err := CreateProject(db, "my-project", "", ".env", user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Update env_file
	err = UpdateProjectEnvFile(db, proj.ID, ".env.local")
	if err != nil {
		t.Fatalf("UpdateProjectEnvFile failed: %v", err)
	}

	// Verify the update
	updated, err := FindProjectByName(db, "my-project")
	if err != nil {
		t.Fatalf("FindProjectByName failed: %v", err)
	}
	if updated.EnvFile != ".env.local" {
		t.Fatalf("expected env_file .env.local, got %s", updated.EnvFile)
	}

	// Update for nonexistent project should fail
	err = UpdateProjectEnvFile(db, "proj_nonexistent", ".env.prod")
	if err == nil {
		t.Fatal("expected error updating nonexistent project, got nil")
	}
}

// Silence the unused import warning — sql is used in TestDeleteProject via db.Exec/QueryRow.
var _ = sql.ErrNoRows
